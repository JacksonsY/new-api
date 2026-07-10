package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// ReliabilitySetting 控制两项可用性防护（fork 自有，对标 sub2api 的
// ratelimit_service / failover_loop）：
//
//   - 429 渠道冷却：上游返回 429 时解析 Retry-After / anthropic-ratelimit-* /
//     x-ratelimit-* 头得到重置时间，将渠道熔断（force-open）到该时刻，避免
//     限流窗口内反复选中同一渠道白白消耗重试预算。头缺失时用默认冷却时长，
//     解析结果始终被钳制在最大冷却时长内，防止毒头把渠道钉死。
//     依赖自适应路由的熔断器生效（enforcement 在 channelhealth.Select）。
//
//   - 同渠道快速重试：请求发送失败（ErrorCodeDoRequestFailed，未收到上游
//     响应的网络层错误）时，先在原渠道短延迟重试，不消耗跨渠道重试次数、
//     不丢优先级、不丢粘性亲和的提示词缓存；仍失败才进入跨渠道 failover。
type ReliabilitySetting struct {
	RateLimitCooldownEnabled        bool `json:"rate_limit_cooldown_enabled"`
	RateLimitCooldownDefaultSeconds int  `json:"rate_limit_cooldown_default_seconds"`
	RateLimitCooldownMaxSeconds     int  `json:"rate_limit_cooldown_max_seconds"`

	SameChannelRetryEnabled bool `json:"same_channel_retry_enabled"`
	SameChannelRetryTimes   int  `json:"same_channel_retry_times"`
	SameChannelRetryDelayMs int  `json:"same_channel_retry_delay_ms"`
}

// 默认全开：429 无头兜底冷却 30s（RPM/TPM 类限流通常在分钟窗口内清零），
// 上限 30 分钟；网络抖动同渠道重试 1 次、间隔 300ms。
var reliabilitySetting = ReliabilitySetting{
	RateLimitCooldownEnabled:        true,
	RateLimitCooldownDefaultSeconds: 30,
	RateLimitCooldownMaxSeconds:     1800,

	SameChannelRetryEnabled: true,
	SameChannelRetryTimes:   1,
	SameChannelRetryDelayMs: 300,
}

func init() {
	config.GlobalConfig.Register("reliability_setting", &reliabilitySetting)
}

func GetReliabilitySetting() *ReliabilitySetting {
	return &reliabilitySetting
}
