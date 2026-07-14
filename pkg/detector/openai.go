package detector

import (
	"context"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const openaiChatPath = "/v1/chat/completions"

// openaiHeaders builds the Bearer-auth header for the Chat Completions API.
func openaiHeaders(apiKey string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + apiKey}
}

// openaiPayload builds a minimal single-user-turn Chat Completions request. Uses
// max_completion_tokens (reasoning models 400 on max_tokens) + temperature 0.
func openaiPayload(model, prompt string, maxTokens int) map[string]interface{} {
	return map[string]interface{}{
		"model":                 model,
		"max_completion_tokens": maxTokens,
		"temperature":           0,
		"messages": []map[string]interface{}{
			{"role": "user", "content": prompt},
		},
	}
}

// openaiFirstChoiceMessage returns choices[0].message, or nil.
func openaiFirstChoiceMessage(resp map[string]interface{}) map[string]interface{} {
	choices := subSlice(resp, "choices")
	if len(choices) == 0 {
		return nil
	}
	c0, _ := choices[0].(map[string]interface{})
	return subMap(c0, "message")
}

// openaiContent returns choices[0].message.content as a string.
func openaiContent(resp map[string]interface{}) string {
	return strField(openaiFirstChoiceMessage(resp), "content")
}

// openaiUsage returns the usage sub-object.
func openaiUsage(resp map[string]interface{}) map[string]interface{} {
	return subMap(resp, "usage")
}

// openaiStream is a reconstructed Chat Completions SSE body.
type openaiStream struct {
	text         string
	usage        map[string]interface{}
	finishReason string
	chunkCount   int
}

func openaiParseStream(objs []map[string]interface{}) openaiStream {
	var s openaiStream
	for _, obj := range objs {
		s.chunkCount++
		if u := subMap(obj, "usage"); u != nil {
			s.usage = u
		}
		for _, raw := range subSlice(obj, "choices") {
			c, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if fr := strField(c, "finish_reason"); fr != "" {
				s.finishReason = fr
			}
			s.text += strField(subMap(c, "delta"), "content")
		}
	}
	return s
}

// openaiStreamProbe issues a streaming request and returns the reconstruction
// plus the raw result (for ok() checks / retry decisions).
func openaiStreamProbe(ctx context.Context, p *prober, body map[string]interface{}) (openaiStream, httpResult) {
	res := p.postSSE(ctx, openaiChatPath, openaiHeaders(p.apiKey), body)
	return openaiParseStream(sseDataObjects(res.text)), res
}

// openaiCollectStream runs the integrity/token stream probe with
// stream_options.include_usage, retrying once without it (some clones reject it).
func openaiCollectStream(ctx context.Context, p *prober, model, prompt string, maxTokens int) (openaiStream, bool) {
	body := openaiPayload(model, prompt, maxTokens)
	body["stream"] = true
	body["stream_options"] = map[string]interface{}{"include_usage": true}
	s, res := openaiStreamProbe(ctx, p, body)
	if res.ok() {
		return s, true
	}
	body2 := openaiPayload(model, prompt, maxTokens)
	body2["stream"] = true
	s2, res2 := openaiStreamProbe(ctx, p, body2)
	return s2, res2.ok()
}

func openaiDetectors() []detectorDef {
	return []detectorDef{
		// 权重与模式成员对齐 Veridrop openai/config.py。protocol 为被动检测器。
		{detectorBasicRequest, "基础请求", 15.0, modeRankQuick, false, openaiBasicRequest},
		{detectorModelConsistency, "模型一致性", 15.0, modeRankQuick, false, openaiModelConsistency},
		{detectorProtocol, "协议规范性", 15.0, modeRankQuick, false, openaiProtocol},
		{detectorFunctionCalling, "函数调用", 15.0, modeRankStandard, false, openaiFunctionCalling},
		{detectorIntegrity, "流式一致性", 15.0, modeRankStandard, false, openaiIntegrity},
		{detectorStructuredOutput, "结构化输出", 15.0, modeRankStandard, false, openaiStructuredOutput},
		{detectorTokenBilling, "Token 计费", 10.0, modeRankStandard, false, openaiTokenBilling},
		{detectorLongContext, "长上下文", 15.0, modeRankFull, true, longContextDetector},
	}
}

// openaiReasoningExhausted mirrors basic_request._looks_like_reasoning_budget_exhausted:
// an empty response that hit finish_reason=length because reasoning tokens ate
// the whole completion budget (gpt-5.x) — not a failure.
func openaiReasoningExhausted(usage map[string]interface{}, finish, text string) bool {
	if text != "" || finish != "length" || usage == nil {
		return false
	}
	comp, ok := intField(usage, "completion_tokens")
	if !ok || comp <= 0 {
		return false
	}
	details := subMap(usage, "completion_tokens_details")
	if details == nil {
		return false
	}
	reasoning, ok := intField(details, "reasoning_tokens")
	return ok && reasoning >= comp
}

// openaiBasicRequest scores instruction following ("pong"): pong→100, any text→80,
// reasoning-budget-exhausted→75, empty→0.
func openaiBasicRequest(ctx context.Context, p *prober, cfg Config) DetectorResult {
	res := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey),
		openaiPayload(cfg.Model, "Reply only with the single word: pong", 96))
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}
	text := openaiContent(res.parsed)
	finish := openaiFinishReason(res.parsed)
	usage := openaiUsage(res.parsed)
	exhausted := openaiReasoningExhausted(usage, finish, text)

	var score float64
	switch {
	case strings.Contains(strings.ToLower(text), "pong"):
		score = 100
	case text != "":
		score = 80
	case exhausted:
		score = 75
	default:
		score = 0
	}
	details := map[string]interface{}{
		"response_text": truncate(text, 300), "object": strField(res.parsed, "object"),
		"model": strField(res.parsed, "model"), "finish_reason": finish,
		"reasoning_budget_exhausted": exhausted,
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: details}
}

// openaiModelConsistency scores model-field match (60) + completion-token CV
// determinism (40/20/0). Quick mode: 1 run, stability skipped with full credit.
func openaiModelConsistency(ctx context.Context, p *prober, cfg Config) DetectorResult {
	return chatModelConsistency(ctx, p, cfg, "模型一致性", false)
}

// chatModelConsistency is the shared OpenAI/Gemini model-field + CV logic
// (gemini's model_info additionally records response id/object).
func chatModelConsistency(ctx context.Context, p *prober, cfg Config, _ string, recordShape bool) DetectorResult {
	runs := 3
	if cfg.Mode == ModeQuick {
		runs = 1
	}
	var responses []map[string]interface{}
	firstErr := ""
	for i := 0; i < runs; i++ {
		res := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey),
			openaiPayload(cfg.Model, "In one sentence, explain HTTP status 418.", 60))
		if res.err != nil || !res.ok() {
			if firstErr == "" {
				if res.err != nil {
					firstErr = res.err.Error()
				} else {
					firstErr = upstreamErrorText(res)
				}
			}
			continue
		}
		responses = append(responses, res.parsed)
	}
	if len(responses) == 0 {
		return DetectorResult{Status: "error", Score: 0, Error: firstErr}
	}

	responseModel := strField(responses[0], "model")
	match := modelMatches(cfg.Model, responseModel)
	score := 0.0
	if match {
		score = 60
	}
	details := map[string]interface{}{
		"request_model": cfg.Model, "response_model": responseModel,
		"model_match": match, "n_runs": runs,
	}
	if recordShape {
		details["response_id"] = strField(responses[0], "id")
		details["response_object"] = strField(responses[0], "object")
	}

	if runs == 1 {
		score += 40
		details["stability_label"] = "skipped_quick_mode"
	} else {
		toks := make([]float64, 0, len(responses))
		seq := make([]int, 0, len(responses))
		for _, r := range responses {
			v, _ := intField(openaiUsage(r), "completion_tokens")
			toks = append(toks, float64(v))
			seq = append(seq, v)
		}
		details["completion_tokens_seq"] = seq
		score += cvStabilityScore(toks, details)
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: details}
}

// cvStabilityScore computes the determinism sub-score from a token sequence's
// coefficient of variation (CV<0.10→40 stable, <0.30→20 suspicious, else 0) and
// records the label/cv onto details.
func cvStabilityScore(toks []float64, details map[string]interface{}) float64 {
	mean := 0.0
	for _, v := range toks {
		mean += v
	}
	if len(toks) > 0 {
		mean /= float64(len(toks))
	}
	if mean <= 0 {
		details["stability_label"] = "no_completion_usage"
		return 0
	}
	var variance float64
	for _, v := range toks {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(toks))
	cv := math.Sqrt(variance) / mean
	details["stability_cv"] = math.Round(cv*1000) / 1000
	switch {
	case cv < 0.10:
		details["stability_label"] = "stable"
		return 40
	case cv < 0.30:
		details["stability_label"] = "suspicious"
		return 20
	default:
		details["stability_label"] = "highly_anomalous"
		return 0
	}
}

// openaiFunctionCalling forces a tool call and scores 5 sub-checks ×20 (has_call,
// id call_ prefix, type function, name match, arguments JSON schema). pass≥70,
// no critical veto (matches function_calling.py).
func openaiFunctionCalling(ctx context.Context, p *prober, cfg Config) DetectorResult {
	const toolName = "get_current_weather"
	payload := openaiPayload(cfg.Model, "Use get_current_weather for Boston, MA in celsius. Do not answer directly.", 128)
	payload["tools"] = []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name": toolName, "description": "Get current weather for a city.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
					"unit": map[string]interface{}{"type": "string", "enum": []string{"celsius", "fahrenheit"}},
				},
				"required":             []string{"city", "unit"},
				"additionalProperties": false,
			},
		},
	}}
	payload["tool_choice"] = map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": toolName}}

	res := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), payload)
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}

	sub := map[string]interface{}{}
	score := 0.0
	msg := openaiFirstChoiceMessage(res.parsed)
	toolCalls := subSlice(msg, "tool_calls")
	hasCall := len(toolCalls) > 0
	sub["has_tool_call"] = map[string]interface{}{"pass": hasCall}
	if !hasCall {
		return DetectorResult{Status: "fail", Score: 0, Details: map[string]interface{}{
			"sub_checks": sub, "finish_reason": openaiFinishReason(res.parsed),
		}}
	}
	score += 20
	call, _ := toolCalls[0].(map[string]interface{})
	cid := strField(call, "id")
	idOK := strings.HasPrefix(cid, "call_")
	sub["id_prefix"] = map[string]interface{}{"value": cid, "pass": idOK}
	if idOK {
		score += 20
	}
	typeOK := strField(call, "type") == "function"
	sub["type"] = map[string]interface{}{"pass": typeOK}
	if typeOK {
		score += 20
	}
	fn := subMap(call, "function")
	nameOK := strField(fn, "name") == toolName
	sub["name"] = map[string]interface{}{"value": strField(fn, "name"), "pass": nameOK}
	if nameOK {
		score += 20
	}
	argsOK := false
	if args := strField(fn, "arguments"); args != "" {
		var parsed map[string]interface{}
		if common.UnmarshalJsonStr(args, &parsed) == nil {
			_, cityStr := parsed["city"].(string)
			unit, _ := parsed["unit"].(string)
			argsOK = cityStr && (unit == "celsius" || unit == "fahrenheit")
		}
	}
	sub["arguments_json"] = map[string]interface{}{"pass": argsOK}
	if argsOK {
		score += 20
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: map[string]interface{}{
		"sub_checks": sub, "finish_reason": openaiFinishReason(res.parsed),
	}}
}

// openaiIntegrity compares non-stream vs stream over 6 sub-checks (non_text 15,
// stream_text 20, text_match 30, finish_match 15, stream_usage 10, usage_match
// 10). pass≥70. maxTokens configurable (openai 32, gemini 128).
func openaiIntegrity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	return chatIntegrity(ctx, p, cfg, 32)
}

func chatIntegrity(ctx context.Context, p *prober, cfg Config, maxTokens int) DetectorResult {
	const prompt = "Reply with exactly: veridrop stream check"
	nonStream := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), openaiPayload(cfg.Model, prompt, maxTokens))
	if nonStream.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: nonStream.err.Error()}
	}
	if !nonStream.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(nonStream)}
	}
	stream, streamOK := openaiCollectStream(ctx, p, cfg.Model, prompt, maxTokens)
	if !streamOK {
		return DetectorResult{Status: "error", Score: 0, Error: "stream probe failed"}
	}

	nonText := strings.TrimSpace(openaiContent(nonStream.parsed))
	streamText := strings.TrimSpace(stream.text)
	nonFinish := openaiFinishReason(nonStream.parsed)
	nonUsage := openaiUsage(nonStream.parsed)

	textMatch := normalizeChatText(nonText) == normalizeChatText(streamText)
	finishMatch := stream.finishReason == nonFinish || stream.finishReason == "" || nonFinish == ""
	usageMatch := usageCloseAll(nonUsage, stream.usage, 1)

	score := 0.0
	if nonText != "" {
		score += 15
	}
	if streamText != "" {
		score += 20
	}
	if textMatch {
		score += 30
	}
	if finishMatch {
		score += 15
	}
	if len(stream.usage) > 0 {
		score += 10
	}
	if usageMatch {
		score += 10
	}
	details := map[string]interface{}{
		"non_stream_text": truncate(nonText, 300), "stream_text": truncate(streamText, 300),
		"text_match": textMatch, "finish_match": finishMatch, "usage_match": usageMatch,
		"non_stream_finish_reason": nonFinish, "stream_finish_reason": stream.finishReason,
		"stream_chunk_count": stream.chunkCount,
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: details}
}

// normalizeChatText mirrors integrity._normalize_text: lowercase, collapse
// whitespace, strip surrounding spaces/dots.
func normalizeChatText(s string) string {
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, " .")
}

// usageCloseAll reports whether at least two of prompt/completion/total tokens
// are within tol between two usage objects (gemini _usage_close, matched>=2).
func usageCloseAll(left, right map[string]interface{}, tol int) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	matched := 0
	for _, k := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		lv, okL := intField(left, k)
		rv, okR := intField(right, k)
		if okL && okR && abs(lv-rv) <= tol {
			matched++
		}
	}
	return matched >= 2
}

// passFail returns "pass" when score >= threshold, else "fail".
func passFail(score, threshold float64) string {
	if score >= threshold {
		return "pass"
	}
	return "fail"
}
