package service

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ParseRateLimitReset 从上游 429 响应头解析限流重置时长（距现在）。
// 解析优先级：
//  1. Retry-After（RFC 7231 标准头：秒数或 HTTP-date）
//  2. anthropic-ratelimit-unified-reset（Claude OAuth 统一窗口，unix 秒）
//  3. 各家 reset/remaining 头对（anthropic RFC3339、OpenAI Go 风格时长、
//     通用 x-ratelimit-reset）：存在 remaining==0 的取其中最大值（等所有已
//     耗尽的窗口都恢复），否则取最小值（最保守的可用下界）。
//
// 单值格式兼容：整数或小数（>1e9 视为 unix 秒时间戳，否则视为秒数）、
// Go duration（"1s"、"6m0s"）、RFC3339、HTTP-date。
// ok=false 表示没有可用的重置信息，调用方应使用默认冷却时长。
func ParseRateLimitReset(header http.Header) (time.Duration, bool) {
	now := time.Now()
	if d, ok := parseResetValue(header.Get("Retry-After"), now); ok {
		return d, true
	}
	if d, ok := parseResetValue(header.Get("anthropic-ratelimit-unified-reset"), now); ok {
		return d, true
	}

	pairs := []struct{ reset, remaining string }{
		{"anthropic-ratelimit-requests-reset", "anthropic-ratelimit-requests-remaining"},
		{"anthropic-ratelimit-tokens-reset", "anthropic-ratelimit-tokens-remaining"},
		{"x-ratelimit-reset-requests", "x-ratelimit-remaining-requests"},
		{"x-ratelimit-reset-tokens", "x-ratelimit-remaining-tokens"},
		{"x-ratelimit-reset", ""},
	}
	var exhaustedMax, anyMin time.Duration
	for _, p := range pairs {
		d, ok := parseResetValue(header.Get(p.reset), now)
		if !ok {
			continue
		}
		if p.remaining != "" && strings.TrimSpace(header.Get(p.remaining)) == "0" && d > exhaustedMax {
			exhaustedMax = d
		}
		if anyMin == 0 || d < anyMin {
			anyMin = d
		}
	}
	if exhaustedMax > 0 {
		return exhaustedMax, true
	}
	if anyMin > 0 {
		return anyMin, true
	}
	return 0, false
}

// parseResetValue 宽容解析单个重置值，返回距 now 的正时长。
// 已经过去的时间点（负时长）返回 ok=false，让调用方走默认冷却。
func parseResetValue(raw string, now time.Time) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		if n <= 0 {
			return 0, false
		}
		if n > 1e9 { // unix 秒时间戳
			d := time.Unix(int64(n), 0).Sub(now)
			if d > 0 {
				return d, true
			}
			return 0, false
		}
		return time.Duration(n * float64(time.Second)), true
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		if d := t.Sub(now); d > 0 {
			return d, true
		}
		return 0, false
	}
	if t, err := http.ParseTime(raw); err == nil {
		if d := t.Sub(now); d > 0 {
			return d, true
		}
	}
	return 0, false
}
