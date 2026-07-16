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

// openaiOmitsTemperature reports whether an OpenAI model rejects `temperature`
// (only the default is accepted). gpt-5.5 is temperature-fixed; sending
// temperature 400s the request, so it is stripped centrally in sanitizeProbeBody
// — mirroring Veridrop's DEFAULT_TEMPERATURE_ONLY_PREFIXES=("gpt-5.5",). gpt-5.6
// (the newer flagship in the same tier, live on veridrop.org but past the local
// snapshot) is treated the same: not stripping and being wrong 400s every probe,
// while stripping when unneeded is harmless. Matches the normalized dotted/hyphen
// forms (gpt-5.5 / gpt-5-5, gpt-5.6 / gpt-5-6) and their variants.
func openaiOmitsTemperature(model string) bool {
	n := normalizeModelID(model)
	return strings.HasPrefix(n, "gpt-5-5") || strings.HasPrefix(n, "gpt-5-6")
}

// grokModel reports whether the target model belongs to xAI's Grok family
// (OpenAI-compatible chat surface; UUID response ids, hidden reasoning on the
// grok-4.x generation).
func grokModel(model string) bool {
	return strings.HasPrefix(normalizeModelID(model), "grok")
}

// openaiReasoningModel reports whether a model is a hidden-reasoning model
// (gpt-5.x, o-series, grok-4.x family). For these a short answer can legitimately
// carry a large, variable completion_tokens count (hidden reasoning that the relay
// may not break out into completion_tokens_details.reasoning_tokens), so
// completion-token bounds and stream/non-stream completion parity are unreliable
// — the prompt-side checks (delta, arithmetic, prompt parity) carry the
// anti-fraud weight instead.
//
// Grok mapping (docs.x.ai, 2026-07): grok-4.5 (configurable effort), the
// grok-4.20 reasoning/multi-agent variants, legacy grok-4/grok-4-fast/
// grok-4.1-fast, grok-code / grok-build and grok-3-mini reason; explicit
// "non-reasoning" variants, grok-3 (non-mini) and grok-2 do not. When a variant
// is ambiguous we err toward reasoning: the loose direction only relaxes one
// billing sub-check, while the strict direction manufactures false fraud
// verdicts (the gpt-5.5 Token-billing incident).
func openaiReasoningModel(model string) bool {
	n := normalizeModelID(model)
	if strings.HasPrefix(n, "grok") {
		switch {
		case strings.Contains(n, "non-reasoning"):
			return false
		case strings.HasPrefix(n, "grok-2"):
			return false
		case strings.HasPrefix(n, "grok-3") && !strings.HasPrefix(n, "grok-3-mini"):
			return false
		default:
			return true
		}
	}
	return strings.HasPrefix(n, "gpt-5") ||
		strings.HasPrefix(n, "o1") || strings.HasPrefix(n, "o3") || strings.HasPrefix(n, "o4")
}

// openaiPayload builds a minimal single-user-turn Chat Completions request. Uses
// max_completion_tokens (reasoning models 400 on max_tokens) + temperature 0.
// temperature is stripped for gpt-5.5 centrally by sanitizeProbeBody.
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
	id           string
	model        string
	created      interface{} // first chunk's created (kept for stream-envelope synthesis)
	text         string
	usage        map[string]interface{}
	finishReason string
	chunkCount   int
}

func openaiParseStream(objs []map[string]interface{}) openaiStream {
	var s openaiStream
	for _, obj := range objs {
		s.chunkCount++
		if v := strField(obj, "id"); v != "" {
			s.id = v
		}
		if v := strField(obj, "model"); v != "" {
			s.model = v
		}
		if s.created == nil {
			if c, ok := obj["created"]; ok {
				s.created = c
			}
		}
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
// plus the raw result (for ok() checks / retry decisions). For Gemini (whose
// client broadcasts synthesized streams to passive detectors) a successful
// stream is also synthesized into a chat.completion envelope and recorded on the
// observation bus. The OpenAI client deliberately does NOT broadcast streams
// (only non-stream responses), so no synthetic observation is recorded there.
func openaiStreamProbe(ctx context.Context, p *prober, body map[string]interface{}) (openaiStream, httpResult) {
	res := p.postSSE(ctx, openaiChatPath, openaiHeaders(p.apiKey), body)
	s := openaiParseStream(sseDataObjects(res.text))
	if p.protocol == ProtocolGemini {
		p.recordChatStreamObservation(s, res)
	}
	return s, res
}

// recordChatStreamObservation reconstructs a non-stream chat.completion envelope
// from a streamed response and puts it on the passive-observation bus, mirroring
// Gemini's _synthesize_stream_response: top-level id/model/created from the first
// chunk, concatenated content, last non-null finish_reason, final usage. The
// keys are always present (matching the reference) so the passive validator sees
// the same shape a genuine relay's stream produces.
func (p *prober) recordChatStreamObservation(s openaiStream, res httpResult) {
	if !res.ok() || s.chunkCount == 0 {
		return
	}
	choice := map[string]interface{}{
		"index":         0,
		"message":       map[string]interface{}{"role": "assistant", "content": s.text},
		"finish_reason": nilIfEmpty(s.finishReason),
	}
	env := map[string]interface{}{
		"id":      nilIfEmpty(s.id),
		"object":  "chat.completion",
		"model":   nilIfEmpty(s.model),
		"created": s.created,
		"choices": []interface{}{choice},
	}
	if len(s.usage) > 0 {
		env["usage"] = s.usage
	}
	p.recordStreamObservation(env, res.statusCode, res.durationMs)
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
		// 身份检测(chat 版,fork 新增):按目标模型厂商(OpenAI/xAI/Google/…)
		// 校验自述身份,异源品牌即换核证据。权重对齐 anthropic 侧 identity。
		{detectorIdentity, "身份检测", 5.0, modeRankQuick, false, chatIdentity},
		{detectorFunctionCalling, "函数调用", 15.0, modeRankStandard, false, openaiFunctionCalling},
		{detectorIntegrity, "流式一致性", 15.0, modeRankStandard, false, openaiIntegrity},
		{detectorStructuredOutput, "结构化输出", 15.0, modeRankStandard, false, openaiStructuredOutput},
		{detectorTokenBilling, "Token 计费", 10.0, modeRankStandard, false, openaiTokenBilling},
		// 协议无关的安全探针组(移植自 LLMprobe-engine, AGPL · full 档):与
		// anthropic 同款,gemini/openai 共用 chat 实现(见 chat_security_probes.go)。
		{detectorSupplyChain, "供应链完整性", 3.0, modeRankFull, false, chatSupplyChainIntegrity},
		{detectorExfilScan, "外泄通道扫描", 3.0, modeRankFull, false, chatExfilScan},
		{detectorAdaptiveInjection, "自适应注入差分", 2.0, modeRankFull, false, chatAdaptiveInjection},
		{detectorHiddenPromptFloor, "隐藏提示地板", 2.0, modeRankFull, false, chatHiddenPromptFloor},
		{detectorUnicodeFidelity, "Unicode 保真", 1.0, modeRankFull, false, chatUnicodeFidelity},
		{detectorSensitiveLeak, "敏感数据泄露", 3.0, modeRankFull, false, chatSensitiveLeak},
		{detectorPkgSubstitution, "包名替换", 3.0, modeRankFull, false, chatPkgSubstitution},
		// Responses API 原生端点探测组(fork 新增):除 chat/completions 外,再探
		// OpenAI/xAI 的第二个原生文本端点 /v1/responses(gpt-5.x/o 系列的真·原生面)。
		// 中转未暴露则 skip(非欺诈);暴露却返回 chat 形状判"非原生实现"。
		{detectorResponsesAPI, "Responses 协议", 2.0, modeRankFull, false, responsesProtocol},
		{detectorResponsesFunc, "Responses 函数调用", 2.0, modeRankFull, false, responsesFunctionCalling},
		// 不再单列 Responses 速度检测器:速度由 chatSpeed 已充分刻画,Responses 面
		// 上的流式速度与之高度冗余,且对只做 chat 的中转还要先 3 次失败尝试才 skip。
		// 后端/网关溯源(chat 版):记录后端 provider + 网关软件为证据(中转剥离
		// 指纹属正常,一律 pass),仅"声称 Gemini 却泄漏 OpenAI 基础设施头"判矛盾。
		{detectorBackendOrigin, "后端溯源", 2.0, modeRankFull, false, chatBackendOrigin},
		// 速度基准(移植自 lm-speed, MIT):真流式 TTFT + 输出 TPS + 单次延迟,恒 pass。
		{detectorSpeed, "速度基准", 1.0, modeRankFull, false, chatSpeed},
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
	// Any run failing hard (transport error or non-2xx after retries) errors the
	// whole detector, matching Veridrop which wraps all N runs in one try/except
	// and returns error on the first raise. Scoring from a subset of survivors
	// would report a determinism verdict computed over fewer samples than asked.
	var responses []map[string]interface{}
	for i := 0; i < runs; i++ {
		res := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey),
			openaiPayload(cfg.Model, "In one sentence, explain HTTP status 418.", 60))
		if res.err != nil {
			return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
		}
		if !res.ok() {
			return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
		}
		responses = append(responses, res.parsed)
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

// usageMatchFn compares a non-stream usage object against a stream usage object
// for the integrity detector; the tolerance is protocol-specific.
type usageMatchFn func(nonStream, stream map[string]interface{}) bool

// openaiIntegrity compares non-stream vs stream over 6 sub-checks (non_text 15,
// stream_text 20, text_match 30, finish_match 15, stream_usage 10, usage_match
// 10). pass≥70. maxTokens configurable (openai 32, gemini 128). OpenAI uses the
// reasoning-tolerant usage comparator (usageCloseOpenAI); Gemini passes the
// ≥2-of-3-within-1 comparator instead.
func openaiIntegrity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	return chatIntegrity(ctx, p, cfg, 32, usageCloseOpenAI)
}

func chatIntegrity(ctx context.Context, p *prober, cfg Config, maxTokens int, usageMatch usageMatchFn) DetectorResult {
	const prompt = "Reply with exactly: veridrop stream check"
	nonStream := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), openaiPayload(cfg.Model, prompt, maxTokens))
	if nonStream.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: nonStream.err.Error()}
	}
	if !nonStream.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(nonStream)}
	}
	// A stream-probe failure is scored from the non-stream side, not reported as
	// a detector error (Veridrop's _collect_stream swallows its own error and the
	// detector proceeds). A relay that serves non-stream but breaks streaming has
	// a real integrity defect the score should reflect, not mask as "error".
	stream, streamOK := openaiCollectStream(ctx, p, cfg.Model, prompt, maxTokens)
	var streamText, streamFinish string
	var streamUsage map[string]interface{}
	streamChunks := 0
	if streamOK {
		streamText = strings.TrimSpace(stream.text)
		streamFinish = stream.finishReason
		streamUsage = stream.usage
		streamChunks = stream.chunkCount
	}

	nonText := strings.TrimSpace(openaiContent(nonStream.parsed))
	nonFinish := openaiFinishReason(nonStream.parsed)
	nonUsage := openaiUsage(nonStream.parsed)

	textMatch := normalizeChatText(nonText) == normalizeChatText(streamText)
	finishMatch := streamFinish == nonFinish || streamFinish == "" || nonFinish == ""
	usageOK := usageMatch(nonUsage, streamUsage)

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
	if len(streamUsage) > 0 {
		score += 10
	}
	if usageOK {
		score += 10
	}
	details := map[string]interface{}{
		"non_stream_text": truncate(nonText, 300), "stream_text": truncate(streamText, 300),
		"text_match": textMatch, "finish_match": finishMatch, "usage_match": usageOK,
		"non_stream_finish_reason": nonFinish, "stream_finish_reason": streamFinish,
		"stream_chunk_count": streamChunks, "stream_ok": streamOK,
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

// usageCloseOpenAI is the OpenAI integrity usage comparator (integrity._usage_close):
// prompt_tokens must match within 1; stream completion_tokens must be present and
// positive; and — because reasoning models allocate hidden reasoning tokens
// differently between stream and non-stream calls — a LOWER stream completion is
// tolerated while over-reporting is rejected (stream ≤ non-stream + max(2, 50%)).
// total_tokens is deliberately not compared.
func usageCloseOpenAI(nonStream, stream map[string]interface{}) bool {
	if len(nonStream) == 0 || len(stream) == 0 {
		return false
	}
	lp, okLP := intField(nonStream, "prompt_tokens")
	rp, okRP := intField(stream, "prompt_tokens")
	if !okLP || !okRP || abs(lp-rp) > 1 {
		return false
	}
	lc, okLC := intField(nonStream, "completion_tokens")
	rc, okRC := intField(stream, "completion_tokens")
	if !okLC || !okRC || rc <= 0 {
		return false
	}
	base := lc
	if base < 1 {
		base = 1
	}
	overTol := base / 2 // int(base * 0.50), truncating like Python
	if overTol < 2 {
		overTol = 2
	}
	return rc <= lc+overTol
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
