package detector

import (
	"context"
	"strings"
)

// Gemini is probed through its OpenAI-compatible surface (POST
// {base}/v1/chat/completions with Bearer auth), so several detectors reuse the
// OpenAI implementations. Where Veridrop's gemini protocol has DISTINCT logic
// (thinking-model token budgets, a shape-fraction protocol validator, no usage
// fingerprint scan), the gemini-specific version is used instead of the reuse.
func geminiDetectors() []detectorDef {
	return []detectorDef{
		// 权重与模式成员对齐 Veridrop gemini/config.py（quick={basic_request,
		// model_info,protocol}，其余 standard+；无 long_context）。
		{detectorBasicRequest, "基础请求", 15.0, modeRankQuick, false, geminiBasicRequest},
		{detectorModelInfo, "模型响应形状", 15.0, modeRankQuick, false, geminiModelInfo},
		{detectorProtocol, "协议规范性", 15.0, modeRankQuick, false, geminiProtocol},
		{detectorFunctionCalling, "函数调用", 15.0, modeRankStandard, false, openaiFunctionCalling},
		{detectorIntegrity, "流式一致性", 15.0, modeRankStandard, false, geminiIntegrity},
		{detectorStructuredOutput, "结构化输出", 15.0, modeRankStandard, false, geminiStructuredOutput},
		{detectorTokenUsage, "Token 用量", 10.0, modeRankStandard, false, geminiTokenUsage},
	}
}

// geminiBasicRequest uses a 64-token budget (Gemini-3 thinking burns ~32 tokens
// before text): pong→100, any text→50, empty→0.
func geminiBasicRequest(ctx context.Context, p *prober, cfg Config) DetectorResult {
	res := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey),
		openaiPayload(cfg.Model, "Reply with exactly: pong", 64))
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}
	text := openaiContent(res.parsed)
	var score float64
	switch {
	case strings.Contains(strings.ToLower(text), "pong"):
		score = 100
	case text != "":
		score = 50
	default:
		score = 0
	}
	details := map[string]interface{}{
		"response_text": truncate(text, 300), "object": strField(res.parsed, "object"),
		"model": strField(res.parsed, "model"), "id": strField(res.parsed, "id"),
		"finish_reason": openaiFinishReason(res.parsed),
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: details}
}

// geminiModelInfo is model-field match + completion-token CV, additionally
// recording response id/object (gemini model_info.py).
func geminiModelInfo(ctx context.Context, p *prober, cfg Config) DetectorResult {
	return chatModelConsistency(ctx, p, cfg, "模型响应形状", true)
}

// geminiIntegrity is the OpenAI stream/non-stream battery with the larger
// 128-token budget Gemini-3 thinking models need.
func geminiIntegrity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	return chatIntegrity(ctx, p, cfg, 128)
}

// --- gemini protocol (passive, shape-fraction) -----------------------------

var geminiRequiredTopLevel = []string{"id", "object", "model", "choices"}

// geminiShapeScore ports gemini protocol._shape_score: a fraction-of-checks
// score (no usage fingerprint scan) with id-prefix/object/finish_reason/usage
// integer checks. Returns (0..100, issues).
func geminiShapeScore(resp map[string]interface{}) (float64, []protoIssue) {
	var issues []protoIssue
	add := func(sev, code, msg string) { issues = append(issues, protoIssue{sev, code, msg}) }
	total, passed := 0, 0

	for _, key := range geminiRequiredTopLevel {
		total++
		if _, ok := resp[key]; ok {
			passed++
		} else {
			add("critical", "missing_"+key, "response missing "+key)
		}
	}
	total++
	if strings.HasPrefix(strField(resp, "id"), "chatcmpl-") {
		passed++
	} else {
		add("major", "bad_id_prefix", "response.id should start with chatcmpl-")
	}
	total++
	if strField(resp, "object") == "chat.completion" {
		passed++
	} else {
		add("major", "bad_object", "response.object should be chat.completion")
	}

	total++
	var first map[string]interface{}
	if choices := subSlice(resp, "choices"); len(choices) > 0 {
		if c0, ok := choices[0].(map[string]interface{}); ok {
			first = c0
			passed++
		} else {
			add("critical", "bad_choices", "choices is not a non-empty array")
		}
	} else {
		add("critical", "bad_choices", "choices is not a non-empty array")
	}

	total++
	if first != nil {
		if openaiValidFinishReasons[strField(first, "finish_reason")] {
			passed++
		} else {
			add("major", "bad_finish_reason", "finish_reason not in allowed enum")
		}
	} else {
		add("major", "bad_finish_reason", "missing finish_reason")
	}

	total++
	if first != nil {
		if m := subMap(first, "message"); m != nil && strField(m, "role") == "assistant" {
			passed++
		} else {
			add("major", "bad_message", "choices[0].message missing or role not assistant")
		}
	} else {
		add("major", "bad_message", "missing message")
	}

	total++
	if usage := subMap(resp, "usage"); usage != nil {
		passed++
		for _, field := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
			total++
			if _, ok := intField(usage, field); ok {
				passed++
			} else {
				add("minor", "bad_usage_"+field, "usage."+field+" is not an integer")
			}
		}
	} else {
		add("major", "missing_usage", "response missing usage object")
	}

	score := 0.0
	if total > 0 {
		score = float64(passed) / float64(total) * 100
	}
	return score, issues
}

// geminiProtocol (passive) averages geminiShapeScore across observed responses.
// passed = avg>=80 AND no critical issue.
func geminiProtocol(_ context.Context, p *prober, _ Config) DetectorResult {
	obs := p.tel.snapshot()
	if len(obs) == 0 {
		return detectorSkip("no-observations")
	}
	total := 0.0
	critCount := 0
	var issueList []map[string]interface{}
	for _, o := range obs {
		score, issues := geminiShapeScore(o.response)
		total += score
		for _, is := range issues {
			if is.severity == "critical" {
				critCount++
			}
			if len(issueList) < 30 {
				issueList = append(issueList, map[string]interface{}{"severity": is.severity, "code": is.code, "message": is.message})
			}
		}
	}
	avg := total / float64(len(obs))
	details := map[string]interface{}{
		"observation_count":    len(obs),
		"critical_issue_count": critCount,
		"issues":               issueList,
	}
	status := "fail"
	if avg >= 80 && critCount == 0 {
		status = "pass"
	}
	return DetectorResult{Status: status, Score: avg, Details: details}
}

// --- gemini token_usage (distinct wide budgets) ----------------------------

const (
	geminiTokenMaxTok   = 128
	geminiDeltaMin      = 45
	geminiDeltaMax      = 140
	geminiArithSlack    = 5
	geminiUsageCloseTol = 2
)

// geminiTokenUsage runs the 5-part usage battery with Gemini-tuned budgets (MAX
// 128 for thinking models, delta 45-140, arithmetic slack ≤5, completion within
// cap+5, stream usage close on ≥2 fields). pass≥80. Deliberately looser than
// OpenAI token_billing — reusing that here would systematically false-fail
// Gemini-3 thinking models.
func geminiTokenUsage(ctx context.Context, p *prober, cfg Config) DetectorResult {
	const shortPrompt = "Reply with exactly: ok"
	longPrompt := shortPrompt + "\n\nReference text:" + strings.Repeat(" apple", 80)

	shortRes := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), openaiPayload(cfg.Model, shortPrompt, geminiTokenMaxTok))
	if shortRes.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: shortRes.err.Error()}
	}
	if !shortRes.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(shortRes)}
	}
	longRes := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), openaiPayload(cfg.Model, longPrompt, geminiTokenMaxTok))
	if longRes.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: longRes.err.Error()}
	}
	if !longRes.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(longRes)}
	}
	stream, _ := openaiCollectStream(ctx, p, cfg.Model, shortPrompt, geminiTokenMaxTok)

	shortUsage := openaiUsage(shortRes.parsed)
	longUsage := openaiUsage(longRes.parsed)
	sub := map[string]interface{}{}
	score := 0.0

	usagePresent := len(shortUsage) > 0 && len(longUsage) > 0
	sub["usage_present"] = map[string]interface{}{"pass": usagePresent}
	if usagePresent {
		score += 20
	}

	arithmeticOK := geminiArithmeticOK(shortUsage) && geminiArithmeticOK(longUsage) &&
		(len(stream.usage) == 0 || geminiArithmeticOK(stream.usage))
	sub["usage_arithmetic"] = map[string]interface{}{"pass": arithmeticOK}
	if arithmeticOK {
		score += 20
	}

	sp, okSP := intField(shortUsage, "prompt_tokens")
	lp, okLP := intField(longUsage, "prompt_tokens")
	deltaOK := false
	if okSP && okLP {
		d := lp - sp
		deltaOK = d >= geminiDeltaMin && d <= geminiDeltaMax
		sub["prompt_token_delta"] = map[string]interface{}{"delta": d, "expected_range": []int{geminiDeltaMin, geminiDeltaMax}, "pass": deltaOK}
	} else {
		sub["prompt_token_delta"] = map[string]interface{}{"pass": false}
	}
	if deltaOK {
		score += 25
	}

	completionOK := geminiCompletionSane(shortUsage) && geminiCompletionSane(longUsage)
	sub["completion_tokens"] = map[string]interface{}{"pass": completionOK}
	if completionOK {
		score += 15
	}

	streamOK := len(stream.usage) > 0 && usageCloseAll(shortUsage, stream.usage, geminiUsageCloseTol)
	sub["stream_usage"] = map[string]interface{}{"stream_usage": stream.usage, "stream_chunk_count": stream.chunkCount, "pass": streamOK}
	if streamOK {
		score += 20
	}

	details := map[string]interface{}{"sub_checks": sub}
	return DetectorResult{Status: passFail(score, 80), Score: score, Details: details}
}

func geminiArithmeticOK(u map[string]interface{}) bool {
	if len(u) == 0 {
		return false
	}
	p, okP := intField(u, "prompt_tokens")
	c, okC := intField(u, "completion_tokens")
	t, okT := intField(u, "total_tokens")
	if !okP || !okC || !okT {
		return false
	}
	return abs(t-(p+c)) <= geminiArithSlack
}

func geminiCompletionSane(u map[string]interface{}) bool {
	c, ok := intField(u, "completion_tokens")
	return ok && c >= 0 && c <= geminiTokenMaxTok+5
}
