package detector

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// --- OpenAI token_billing (weight 10) ---
// Ported from protocols/openai/detectors/token_billing.py. The live official
// "reference" baseline (Tier C) is intentionally omitted: with no reference
// available the Python code takes the comparison_available=false path, which
// never caps the score, so we reproduce that path exactly.

const (
	obMaxTokens       = 8
	obDeltaMin        = 45
	obDeltaMax        = 130
	obCompletionMaxOK = 12
)

func openaiTokenBilling(ctx context.Context, p *prober, cfg Config) DetectorResult {
	headers := openaiHeaders(p.apiKey)

	short := p.postJSON(ctx, openaiChatPath, headers, openaiChatPayloadMC(cfg.Model, tokenShortPrompt, obMaxTokens))
	if r, bad := tokenRequestError(short); bad {
		return r
	}
	long := p.postJSON(ctx, openaiChatPath, headers, openaiChatPayloadMC(cfg.Model, tokenLongPrompt(), obMaxTokens))
	if r, bad := tokenRequestError(long); bad {
		return r
	}

	streamPayload := openaiChatPayloadMC(cfg.Model, tokenShortPrompt, obMaxTokens)
	streamPayload["stream"] = true
	streamPayload["stream_options"] = map[string]interface{}{"include_usage": true}
	streamRes := p.postSSE(ctx, openaiChatPath, headers, streamPayload)
	streamUsage, chunkCount, streamErr := collectStreamUsage(streamRes)

	shortUsage := subMap(short.parsed, "usage")
	longUsage := subMap(long.parsed, "usage")
	shortText := strings.TrimSpace(openaiContent(short.parsed))
	longText := strings.TrimSpace(openaiContent(long.parsed))

	sub := map[string]interface{}{}
	score := 0.0

	usagePresent := len(shortUsage) > 0 && len(longUsage) > 0
	sub["usage_present"] = map[string]interface{}{"pass": usagePresent, "short_usage": shortUsage, "long_usage": longUsage}
	if usagePresent {
		score += 20
	}

	arithmeticOK := usageArithmeticExact(shortUsage) && usageArithmeticExact(longUsage) &&
		(len(streamUsage) == 0 || usageArithmeticExact(streamUsage))
	sub["usage_arithmetic"] = map[string]interface{}{"pass": arithmeticOK, "note": "total_tokens 应等于 prompt_tokens + completion_tokens"}
	if arithmeticOK {
		score += 20
	}

	sp, spOK := tokenField(shortUsage, "prompt_tokens")
	lp, lpOK := tokenField(longUsage, "prompt_tokens")
	var promptDelta interface{}
	deltaOK := false
	if spOK && lpOK {
		d := lp - sp
		promptDelta = d
		deltaOK = obDeltaMin <= d && d <= obDeltaMax
	}
	sub["prompt_token_delta"] = map[string]interface{}{
		"short_prompt_tokens": intOrNil(sp, spOK),
		"long_prompt_tokens":  intOrNil(lp, lpOK),
		"delta":               promptDelta,
		"expected_range":      []int{obDeltaMin, obDeltaMax},
		"pass":                deltaOK,
	}
	if deltaOK {
		score += 25
	}

	completionOK := completionSaneBounded(shortUsage, 1, obCompletionMaxOK) && completionSaneBounded(longUsage, 1, obCompletionMaxOK)
	sub["completion_tokens"] = map[string]interface{}{
		"short_completion_tokens": rawUsageField(shortUsage, "completion_tokens"),
		"long_completion_tokens":  rawUsageField(longUsage, "completion_tokens"),
		"short_text":              truncate(shortText, 80),
		"long_text":               truncate(longText, 80),
		"pass":                    completionOK,
	}
	if completionOK {
		score += 15
	}

	streamOK := false
	streamNote := ""
	if len(streamUsage) == 0 {
		streamNote = "接口没有返回流式 usage,无法用流式结果交叉验证"
	} else {
		streamOK = streamUsageCompatibleOpenAI(shortUsage, streamUsage)
	}
	sub["stream_usage"] = map[string]interface{}{
		"non_stream_usage":   shortUsage,
		"stream_usage":       streamUsage,
		"stream_chunk_count": chunkCount,
		"stream_error":       nilIfEmpty(streamErr),
		"pass":               streamOK,
		"note":               streamNote,
	}
	if streamOK {
		score += 20
	}

	// No live reference available: reproduce the comparison_available=false
	// branch, which counts as a passing sub-check and never caps the score.
	sub["normal_usage"] = map[string]interface{}{
		"comparison_available": false,
		"pass":                 true,
		"score":                100.0,
		"note":                 "仅做本次响应自洽检查",
	}

	if !usagePresent || countPassedSubChecks(sub) <= 1 {
		return detectorSkip("insufficient-token-usage")
	}

	riskLevel := "high"
	if score >= 90 {
		riskLevel = "low"
	} else if score >= 70 {
		riskLevel = "medium"
	}

	details := map[string]interface{}{
		"sub_checks":    sub,
		"risk_level":    riskLevel,
		"evaluation_zh": tokenBillingEvaluation(score, len(streamUsage) > 0),
	}
	status := "pass"
	if score < 90 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}

func tokenBillingEvaluation(score float64, hasStreamUsage bool) string {
	if score >= 90 {
		return "Token 数基本可信: usage 加法自洽,加长文本带来的 token 增量合理,流式和非流式统计也能对上。"
	}
	if score >= 70 {
		if !hasStreamUsage {
			return "Token 数有偏差: 基础统计基本可用,但接口没有给出完整流式 usage,无法做更强交叉验证。"
		}
		return "Token 数有偏差: 有部分检查没有对上,建议留意是否存在额外提示词、适配层统计误差或轻微多算。"
	}
	return "Token 数明显异常: usage 字段、token 增量或流式/非流式统计存在明显问题,有虚报或统计错误风险。"
}

// usageArithmeticExact reports total == prompt + completion with all three
// present as integers (no slack — the OpenAI variant).
func usageArithmeticExact(usage map[string]interface{}) bool {
	p, pOK := tokenField(usage, "prompt_tokens")
	c, cOK := tokenField(usage, "completion_tokens")
	t, tOK := tokenField(usage, "total_tokens")
	return pOK && cOK && tOK && p+c == t
}

// completionSaneBounded reports 0<c(or lo<=c)<=hi. lo=1 enforces strictly
// positive (OpenAI); lo=0 allows zero (Gemini thinking models).
func completionSaneBounded(usage map[string]interface{}, lo, hi int) bool {
	c, ok := tokenField(usage, "completion_tokens")
	return ok && lo <= c && c <= hi
}

// streamUsageCompatibleOpenAI ports token_billing.py:_stream_usage_compatible.
func streamUsageCompatibleOpenAI(nonStream, stream map[string]interface{}) bool {
	if len(nonStream) == 0 || len(stream) == 0 {
		return false
	}
	lp, lpOK := tokenField(nonStream, "prompt_tokens")
	rp, rpOK := tokenField(stream, "prompt_tokens")
	if !lpOK || !rpOK || abs(lp-rp) > 1 {
		return false
	}
	lc, lcOK := tokenField(nonStream, "completion_tokens")
	rc, rcOK := tokenField(stream, "completion_tokens")
	if !lcOK || !rcOK || rc <= 0 {
		return false
	}
	if rc > obCompletionMaxOK {
		return false
	}
	overTolerance := max(2, int(float64(max(lc, 1))*0.50))
	return rc <= lc+overTolerance
}

func rawUsageField(usage map[string]interface{}, key string) interface{} {
	if usage == nil {
		return nil
	}
	return usage[key]
}

// collectStreamUsage extracts stream usage from an SSE probe result, treating
// any stream failure as non-fatal (matching Python's _collect_stream_usage).
func collectStreamUsage(res httpResult) (usage map[string]interface{}, chunkCount int, errStr string) {
	if res.err != nil {
		return nil, 0, res.err.Error()
	}
	if !res.ok() {
		return nil, 0, upstreamErrorText(res)
	}
	usage, chunkCount = streamUsageFromSSE(res.text)
	return usage, chunkCount, ""
}

// tokenRequestError maps a fatal (short/long) probe failure to an error result.
func tokenRequestError(res httpResult) (DetectorResult, bool) {
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error(),
			Details: map[string]interface{}{"status_code": res.statusCode}}, true
	}
	if !res.ok() {
		msg := upstreamErrorText(res)
		return DetectorResult{Status: "error", Score: 0, Error: msg,
			Details: map[string]interface{}{"status_code": res.statusCode, "error": msg}}, true
	}
	return DetectorResult{}, false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// --- OpenAI token_parity (weight 10) ---
// Ported from protocols/openai/detectors/token_parity.py. This is a baseline
// comparison (Tier C): no baseline is bundled, so it looks only at the
// optional VERIDROP_OPENAI_BASELINE_PATH override and otherwise skips — exactly
// as the Python does when no official baseline exists.

const tokenParityPrompt = "Reply with exactly: pong"

func openaiTokenParity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	baseline := loadOpenAIBaselineUsage(cfg.Model, cfg.Mode)
	if baseline == nil {
		return detectorSkip("no-openai-official-baseline")
	}

	payload := map[string]interface{}{
		"model":                 cfg.Model,
		"max_completion_tokens": 32,
		"temperature":           0,
		"store":                 false,
		"messages": []map[string]interface{}{
			{"role": "user", "content": tokenParityPrompt},
		},
	}
	res := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), payload)
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error(),
			Details: map[string]interface{}{"status_code": res.statusCode}}
	}
	if !res.ok() {
		msg := upstreamErrorText(res)
		return DetectorResult{Status: "error", Score: 0, Error: msg,
			Details: map[string]interface{}{"status_code": res.statusCode, "error": msg}}
	}

	usage := subMap(res.parsed, "usage")
	score, diffs := scoreTokenParity(usage, baseline)
	status := "pass"
	if score < 80 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: map[string]interface{}{
		"usage":          usage,
		"baseline_usage": baseline,
		"diffs":          diffs,
	}}
}

// loadOpenAIBaselineUsage loads an official usage baseline. Only the explicit
// env override is honored here (no baselines are bundled); a missing/invalid
// file yields nil so the detector skips.
func loadOpenAIBaselineUsage(model, mode string) map[string]int {
	var candidates []string
	if explicit := strings.TrimSpace(os.Getenv("VERIDROP_OPENAI_BASELINE_PATH")); explicit != "" {
		candidates = append(candidates, explicit)
	}
	for _, suffix := range []string{"chat_text", "full", mode} {
		if suffix != "" {
			candidates = append(candidates, filepath.Join("data", "baselines", "openai", model+"_"+suffix+".json"))
		}
	}
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var data map[string]interface{}
		if common.Unmarshal(raw, &data) != nil {
			continue
		}
		if usage := extractBaselineUsage(data); usage != nil {
			return usage
		}
	}
	return nil
}

func extractBaselineUsage(data map[string]interface{}) map[string]int {
	if u := usageInts(subMap(data, "usage")); u != nil {
		return u
	}
	for _, raw := range subSlice(data, "probes") {
		probe, ok := raw.(map[string]interface{})
		if !ok || strField(probe, "wire_api") != "chat_completions" {
			continue
		}
		if u := usageInts(subMap(probe, "response")); u != nil {
			return u
		}
	}
	return nil
}

// usageInts extracts prompt/completion/total token ints, returning nil unless
// all three are present as integers (matches token_parity.py:_usage_ints).
func usageInts(m map[string]interface{}) map[string]int {
	usage := m
	if inner := subMap(m, "usage"); inner != nil {
		usage = inner
	}
	out := map[string]int{}
	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		v, ok := tokenField(usage, key)
		if !ok {
			return nil
		}
		out[key] = v
	}
	return out
}

func scoreTokenParity(usage map[string]interface{}, baseline map[string]int) (float64, map[string]interface{}) {
	diffs := map[string]interface{}{}
	score := 100.0
	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		observed, ok := tokenField(usage, key)
		expected := baseline[key]
		if !ok {
			diffs[key] = map[string]interface{}{"observed": rawUsageField(usage, key), "expected": expected, "delta": nil, "pass": false}
			score -= 35
			continue
		}
		delta := observed - expected
		tolerance := 1
		if key == "total_tokens" {
			tolerance = 2
		}
		okDelta := abs(delta) <= tolerance
		diffs[key] = map[string]interface{}{"observed": observed, "expected": expected, "delta": delta, "pass": okDelta}
		if !okDelta {
			if key == "prompt_tokens" {
				score -= 25
			} else {
				score -= 15
			}
		}
	}
	if score < 0 {
		score = 0
	}
	return score, diffs
}
