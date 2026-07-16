package detector

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Gemini is probed through Google's NATIVE generateContent protocol (see
// gemini_native.go). Every detector below inspects native fingerprints —
// candidates[].content.parts[], usageMetadata (promptTokenCount /
// candidatesTokenCount / thoughtsTokenCount), modelVersion, the native
// finishReason enum — instead of the OpenAI-compat shim that would erase them.
// The protocol-agnostic security/behaviour probes reach Gemini through the
// prober's native dispatch (probeChat / chatContent), so the whole battery
// speaks Google's wire; a response leaking OpenAI-shape fields while claiming
// Gemini is treated as swapped-core evidence.
func geminiDetectors() []detectorDef {
	return []detectorDef{
		// 权重与模式成员对齐 Veridrop gemini/config.py（quick={basic_request,
		// model_info,protocol}，其余 standard+；无 long_context）。
		{detectorBasicRequest, "基础请求", 15.0, modeRankQuick, false, geminiBasicRequest},
		{detectorModelInfo, "模型响应形状", 15.0, modeRankQuick, false, geminiModelInfo},
		{detectorProtocol, "协议规范性", 15.0, modeRankQuick, false, geminiProtocol},
		// 身份检测(chat 版,fork 新增):gemini 目标自述 Gemini/Google 为真,
		// 异源品牌(ChatGPT/Claude/Grok…)即换核证据。走原生分发。
		{detectorIdentity, "身份检测", 5.0, modeRankQuick, false, chatIdentity},
		{detectorFunctionCalling, "函数调用", 15.0, modeRankStandard, false, geminiFunctionCalling},
		{detectorIntegrity, "流式一致性", 15.0, modeRankStandard, false, geminiIntegrity},
		{detectorStructuredOutput, "结构化输出", 15.0, modeRankStandard, false, geminiStructuredOutput},
		{detectorTokenUsage, "Token 用量", 10.0, modeRankStandard, false, geminiTokenUsage},
		// 协议无关的安全探针组(移植自 LLMprobe-engine, AGPL · full 档):经 prober
		// 原生分发打到 generateContent,与 openai 共用扫描逻辑(见 chat_security_probes.go)。
		{detectorSupplyChain, "供应链完整性", 3.0, modeRankFull, false, chatSupplyChainIntegrity},
		{detectorExfilScan, "外泄通道扫描", 3.0, modeRankFull, false, chatExfilScan},
		{detectorAdaptiveInjection, "自适应注入差分", 2.0, modeRankFull, false, chatAdaptiveInjection},
		{detectorHiddenPromptFloor, "隐藏提示地板", 2.0, modeRankFull, false, chatHiddenPromptFloor},
		{detectorUnicodeFidelity, "Unicode 保真", 1.0, modeRankFull, false, chatUnicodeFidelity},
		{detectorSensitiveLeak, "敏感数据泄露", 3.0, modeRankFull, false, chatSensitiveLeak},
		{detectorPkgSubstitution, "包名替换", 3.0, modeRankFull, false, chatPkgSubstitution},
		// 后端/网关溯源(chat 版):记录后端 provider + 网关软件为证据;gemini 侧
		// 若响应泄漏 OpenAI 独有基础设施头,判"拿 OpenAI 后端冒充 Gemini"。
		{detectorBackendOrigin, "后端溯源", 2.0, modeRankFull, false, chatBackendOrigin},
		// 速度基准(移植自 lm-speed, MIT):原生流式 TTFT + 输出 TPS + 单次延迟,恒 pass。
		{detectorSpeed, "速度基准", 1.0, modeRankFull, false, chatSpeed},
	}
}

// geminiBasicRequest uses a 64-token budget (Gemini-3 thinking burns ~32 tokens
// before text): pong→100, any text→50, empty→0. Records native fingerprints
// (modelVersion / responseId / finishReason).
func geminiBasicRequest(ctx context.Context, p *prober, cfg Config) DetectorResult {
	res := p.postJSON(ctx, geminiNativePath(cfg.Model, false), geminiNativeHeaders(p.apiKey),
		geminiNativeBody("Reply with exactly: pong", 64))
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}
	text := geminiNativeText(res.parsed)
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
		"response_text": truncate(text, 300),
		"model_version": geminiNativeModelVersion(res.parsed),
		"response_id":   geminiNativeResponseID(res.parsed),
		"finish_reason": geminiNativeFinish(res.parsed),
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: details}
}

// geminiModelInfo scores native modelVersion match (60) plus determinism:
// candidatesTokenCount coefficient-of-variation across runs (CV<0.10→40,
// <0.30→20, else 0). Quick mode uses one run and skips the stability test with
// full credit.
func geminiModelInfo(ctx context.Context, p *prober, cfg Config) DetectorResult {
	runs := 3
	if cfg.Mode == ModeQuick {
		runs = 1
	}
	var responses []map[string]interface{}
	for i := 0; i < runs; i++ {
		res := p.postJSON(ctx, geminiNativePath(cfg.Model, false), geminiNativeHeaders(p.apiKey),
			geminiNativeBody("In one sentence, explain HTTP status 418.", 60))
		if res.err != nil {
			return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
		}
		if !res.ok() {
			return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
		}
		responses = append(responses, res.parsed)
	}

	// modelVersion is Gemini's native model identifier (e.g. "gemini-2.5-flash").
	responseModel := geminiNativeModelVersion(responses[0])
	match := modelMatches(cfg.Model, responseModel)
	score := 0.0
	if match {
		score = 60
	}
	details := map[string]interface{}{
		"request_model": cfg.Model, "response_model": responseModel,
		"model_match": match, "n_runs": runs,
		"response_id": geminiNativeResponseID(responses[0]),
	}

	if runs == 1 {
		score += 40
		details["stability_label"] = "skipped_quick_mode"
	} else {
		toks := make([]float64, 0, len(responses))
		seq := make([]int, 0, len(responses))
		for _, r := range responses {
			v, _ := intField(geminiNativeUsage(r), "candidatesTokenCount")
			toks = append(toks, float64(v))
			seq = append(seq, v)
		}
		details["candidates_tokens_seq"] = seq
		score += cvStabilityScore(toks, details)
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: details}
}

// --- gemini protocol (passive, native shape) -------------------------------

// geminiValidFinish is Google's native finishReason enum; "" is allowed for an
// intermediate/streamed chunk.
var geminiValidFinish = map[string]bool{
	"": true, "STOP": true, "MAX_TOKENS": true, "SAFETY": true, "RECITATION": true,
	"LANGUAGE": true, "OTHER": true, "BLOCKLIST": true, "PROHIBITED_CONTENT": true,
	"SPII": true, "MALFORMED_FUNCTION_CALL": true, "IMAGE_SAFETY": true,
	"UNEXPECTED_TOOL_CALL": true, "FINISH_REASON_UNSPECIFIED": true,
}

// geminiOpenAILeakKeys are OpenAI-envelope fields a genuine Gemini endpoint never
// emits; their presence in a "gemini" response is swapped-core evidence.
var geminiOpenAILeakKeys = []string{"choices", "system_fingerprint"}

// geminiNativeShapeScore validates a native generateContent envelope: a
// fraction-of-checks score over candidates/content/parts/role/finishReason/
// usageMetadata/modelVersion, plus a critical swapped-core flag when the
// response leaks OpenAI-shape fields. Returns (0..100, issues).
func geminiNativeShapeScore(resp map[string]interface{}) (float64, []protoIssue) {
	var issues []protoIssue
	add := func(sev, code, msg string) { issues = append(issues, protoIssue{sev, code, msg}) }
	total, passed := 0, 0
	check := func(ok bool) {
		total++
		if ok {
			passed++
		}
	}

	// Swapped-core leak: OpenAI envelope fields on a Gemini response.
	for _, k := range geminiOpenAILeakKeys {
		if _, ok := resp[k]; ok {
			add("critical", "openai_shape_leak", "Gemini response leaks OpenAI-only field "+k+" — 疑似拿 OpenAI 后端冒充 Gemini")
		}
	}
	if strField(resp, "object") == "chat.completion" {
		add("critical", "openai_shape_leak", "Gemini response carries object=chat.completion — 疑似 OpenAI 兼容层冒充原生")
	}

	cand := geminiFirstCandidate(resp)
	check(cand != nil)
	if cand == nil {
		add("critical", "missing_candidates", "response has no candidates[]")
	}

	content := subMap(cand, "content")
	check(content != nil)
	if content == nil && cand != nil {
		add("critical", "missing_content", "candidate has no content object")
	}

	parts := subSlice(content, "parts")
	check(len(parts) > 0)
	if len(parts) == 0 && content != nil {
		add("major", "missing_parts", "content.parts is empty")
	}

	// Native assistant role is "model" (not "assistant"). role is omitempty in the
	// schema (dto.GeminiChatContent), so an ABSENT role is spec-valid; only a
	// present-but-wrong role is a signal.
	role := strField(content, "role")
	check(role == "" || role == "model")
	if role != "" && role != "model" {
		add("minor", "bad_role", "content.role should be model when present")
	}

	check(geminiValidFinish[geminiNativeFinish(resp)])
	if !geminiValidFinish[geminiNativeFinish(resp)] {
		add("major", "bad_finish_reason", "finishReason not in the native enum")
	}

	check(strField(resp, "modelVersion") != "")
	if strField(resp, "modelVersion") == "" {
		add("minor", "missing_model_version", "response missing modelVersion")
	}

	usage := geminiNativeUsage(resp)
	check(len(usage) > 0)
	if len(usage) > 0 {
		for _, field := range []string{"promptTokenCount", "candidatesTokenCount", "totalTokenCount"} {
			check(isNonNegInt(usage[field]))
			if !isNonNegInt(usage[field]) {
				add("minor", "bad_usage_"+field, "usageMetadata."+field+" is not a non-negative integer")
			}
		}
	} else {
		add("major", "missing_usage", "response missing usageMetadata")
	}

	score := 0.0
	if total > 0 {
		score = float64(passed) / float64(total) * 100
	}
	return score, issues
}

// geminiProtocol (passive) averages geminiNativeShapeScore across observed native
// responses. passed = avg>=80 AND no critical issue.
func geminiProtocol(_ context.Context, p *prober, _ Config) DetectorResult {
	obs := p.tel.snapshot()
	if len(obs) == 0 {
		return detectorSkip("no-observations")
	}
	total := 0.0
	critCount := 0
	var issueList []map[string]interface{}
	for _, o := range obs {
		score, issues := geminiNativeShapeScore(o.response)
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

// --- gemini integrity (native stream vs non-stream) ------------------------

const geminiIntegrityMaxTok = 128

// geminiIntegrity compares a native non-stream response against a native
// streamGenerateContent response over the same 6 sub-checks as chatIntegrity
// (non_text 15, stream_text 20, text_match 30, finish_match 15, stream_usage 10,
// usage_match 10), using the ≥2-of-3-within-1 native usage comparator. pass≥70.
func geminiIntegrity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	const prompt = "Reply with exactly: veridrop stream check"
	nonStream := p.postJSON(ctx, geminiNativePath(cfg.Model, false), geminiNativeHeaders(p.apiKey),
		geminiNativeBody(prompt, geminiIntegrityMaxTok))
	if nonStream.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: nonStream.err.Error()}
	}
	if !nonStream.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(nonStream)}
	}

	streamRes := p.postSSE(ctx, geminiNativePath(cfg.Model, true), geminiNativeHeaders(p.apiKey),
		geminiNativeBody(prompt, geminiIntegrityMaxTok))
	streamObjs := sseDataObjects(streamRes.text)
	streamOK := streamRes.ok() && len(streamObjs) > 0
	var streamText, streamFinish string
	var streamUsage map[string]interface{}
	if streamOK {
		streamText = strings.TrimSpace(geminiStreamText(streamObjs))
		streamFinish = geminiStreamFinish(streamObjs)
		streamUsage = geminiStreamUsage(streamObjs)
	}

	nonText := strings.TrimSpace(geminiNativeText(nonStream.parsed))
	nonFinish := geminiNativeFinish(nonStream.parsed)
	nonUsage := geminiNativeUsage(nonStream.parsed)

	textMatch := normalizeChatText(nonText) == normalizeChatText(streamText)
	finishMatch := streamFinish == nonFinish || streamFinish == "" || nonFinish == ""
	usageOK := geminiUsageCloseNative(nonUsage, streamUsage, 1)

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
		"stream_chunk_count": len(streamObjs), "stream_ok": streamOK,
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: details}
}

// geminiUsageCloseNative reports whether at least two of promptTokenCount /
// candidatesTokenCount / totalTokenCount agree within tol across two native
// usageMetadata objects.
func geminiUsageCloseNative(a, b map[string]interface{}, tol int) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	agree := 0
	for _, k := range []string{"promptTokenCount", "candidatesTokenCount", "totalTokenCount"} {
		av, okA := intField(a, k)
		bv, okB := intField(b, k)
		if okA && okB && abs(av-bv) <= tol {
			agree++
		}
	}
	return agree >= 2
}

// --- gemini token_usage (native usageMetadata) -----------------------------

const (
	geminiTokenMaxTok = 128
	geminiDeltaMin    = 45
	geminiDeltaMax    = 140
	geminiArithSlack  = 5
)

// geminiTokenUsage runs the 5-part usage battery on native usageMetadata:
// present (20), arithmetic total≈prompt+candidates+thoughts within 5 (20),
// prompt-token delta short→long in [45,140] (25), candidates within cap+5 (15),
// stream usage close on ≥2-of-3 (20). pass≥80. Deliberately looser than OpenAI
// token_billing (thinking models allocate variable thoughtsTokenCount).
func geminiTokenUsage(ctx context.Context, p *prober, cfg Config) DetectorResult {
	const shortPrompt = "Reply with exactly: ok"
	longPrompt := shortPrompt + "\n\nReference text:" + strings.Repeat(" apple", 80)

	shortRes := p.postJSON(ctx, geminiNativePath(cfg.Model, false), geminiNativeHeaders(p.apiKey), geminiNativeBody(shortPrompt, geminiTokenMaxTok))
	if shortRes.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: shortRes.err.Error()}
	}
	if !shortRes.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(shortRes)}
	}
	longRes := p.postJSON(ctx, geminiNativePath(cfg.Model, false), geminiNativeHeaders(p.apiKey), geminiNativeBody(longPrompt, geminiTokenMaxTok))
	if longRes.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: longRes.err.Error()}
	}
	if !longRes.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(longRes)}
	}
	streamRes := p.postSSE(ctx, geminiNativePath(cfg.Model, true), geminiNativeHeaders(p.apiKey), geminiNativeBody(shortPrompt, geminiTokenMaxTok))
	streamUsage := geminiStreamUsage(sseDataObjects(streamRes.text))

	shortUsage := geminiNativeUsage(shortRes.parsed)
	longUsage := geminiNativeUsage(longRes.parsed)
	sub := map[string]interface{}{}
	score := 0.0

	usagePresent := len(shortUsage) > 0 && len(longUsage) > 0
	sub["usage_present"] = map[string]interface{}{"pass": usagePresent}
	if usagePresent {
		score += 20
	}

	arithmeticOK := geminiArithmeticOK(shortUsage) && geminiArithmeticOK(longUsage) &&
		(len(streamUsage) == 0 || geminiArithmeticOK(streamUsage))
	sub["usage_arithmetic"] = map[string]interface{}{"pass": arithmeticOK}
	if arithmeticOK {
		score += 20
	}

	sp, okSP := intField(shortUsage, "promptTokenCount")
	lp, okLP := intField(longUsage, "promptTokenCount")
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

	completionOK := geminiCandidatesSane(shortUsage) && geminiCandidatesSane(longUsage)
	sub["candidates_tokens"] = map[string]interface{}{"pass": completionOK}
	if completionOK {
		score += 15
	}

	streamOK := len(streamUsage) > 0 && geminiUsageCloseNative(shortUsage, streamUsage, 2)
	sub["stream_usage"] = map[string]interface{}{"stream_usage": streamUsage, "pass": streamOK}
	if streamOK {
		score += 20
	}

	details := map[string]interface{}{"sub_checks": sub}
	return DetectorResult{Status: passFail(score, 80), Score: score, Details: details}
}

// geminiArithmeticOK checks totalTokenCount ≈ prompt + candidates + thoughts
// (native total includes the thinking tokens) within slack.
func geminiArithmeticOK(u map[string]interface{}) bool {
	if len(u) == 0 {
		return false
	}
	prompt, okP := intField(u, "promptTokenCount")
	cand, okC := intField(u, "candidatesTokenCount")
	total, okT := intField(u, "totalTokenCount")
	if !okP || !okC || !okT {
		return false
	}
	thoughts, _ := intField(u, "thoughtsTokenCount")
	return abs(total-(prompt+cand+thoughts)) <= geminiArithSlack
}

// geminiCandidatesSane checks the visible-output token count is bounded by the
// requested max (thoughtsTokenCount is separate and intentionally unbounded).
func geminiCandidatesSane(u map[string]interface{}) bool {
	c, ok := intField(u, "candidatesTokenCount")
	return ok && c >= 0 && c <= geminiTokenMaxTok+5
}

// --- gemini function calling (native tool use) -----------------------------

// geminiFunctionCalling forces a native tool call (tools.functionDeclarations +
// toolConfig ANY) and scores has_call (40) / name match (30) / arguments (30).
// Native functionCall.args is already a JSON object (no string to parse) and
// there is no call-id/type, so the sub-checks differ from the OpenAI shape.
func geminiFunctionCalling(ctx context.Context, p *prober, cfg Config) DetectorResult {
	const toolName = "get_current_weather"
	body := geminiNativeBody("Use get_current_weather for Boston, MA in celsius. Do not answer directly.", 128)
	body["tools"] = []map[string]interface{}{{
		"functionDeclarations": []map[string]interface{}{{
			"name": toolName, "description": "Get current weather for a city.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
					"unit": map[string]interface{}{"type": "string", "enum": []string{"celsius", "fahrenheit"}},
				},
				"required": []string{"city", "unit"},
			},
		}},
	}}
	body["toolConfig"] = map[string]interface{}{
		"functionCallingConfig": map[string]interface{}{"mode": "ANY", "allowedFunctionNames": []string{toolName}},
	}

	res := p.postJSON(ctx, geminiNativePath(cfg.Model, false), geminiNativeHeaders(p.apiKey), body)
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}

	sub := map[string]interface{}{}
	score := 0.0
	fc := geminiFunctionCallPart(res.parsed)
	hasCall := fc != nil
	sub["has_tool_call"] = map[string]interface{}{"pass": hasCall}
	if !hasCall {
		return DetectorResult{Status: "fail", Score: 0, Details: map[string]interface{}{
			"sub_checks": sub, "finish_reason": geminiNativeFinish(res.parsed),
		}}
	}
	score += 40
	nameOK := strField(fc, "name") == toolName
	sub["name"] = map[string]interface{}{"value": strField(fc, "name"), "pass": nameOK}
	if nameOK {
		score += 30
	}
	args := subMap(fc, "args")
	_, cityStr := args["city"].(string)
	unit, _ := args["unit"].(string)
	argsOK := cityStr && (unit == "celsius" || unit == "fahrenheit")
	sub["arguments"] = map[string]interface{}{"pass": argsOK}
	if argsOK {
		score += 30
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: map[string]interface{}{
		"sub_checks": sub, "finish_reason": geminiNativeFinish(res.parsed),
	}}
}

// --- gemini structured output (native responseSchema) ----------------------

// geminiStructuredOutput asks for JSON via the native
// generationConfig.responseMimeType + responseSchema and verifies the response
// is pure JSON matching the schema. 384 leaves room for Gemini-3 reasoning.
func geminiStructuredOutput(ctx context.Context, p *prober, cfg Config) DetectorResult {
	const nonce = "gemini-detector"
	body := geminiNativeBody(fmt.Sprintf(`Return JSON with ok=true and nonce="%s".`, nonce), 384)
	body["generationConfig"] = map[string]interface{}{
		"maxOutputTokens":  384,
		"temperature":      0,
		"responseMimeType": "application/json",
		"responseSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"ok":    map[string]interface{}{"type": "boolean"},
				"nonce": map[string]interface{}{"type": "string"},
			},
			"required": []string{"ok", "nonce"},
		},
	}

	res := p.postJSON(ctx, geminiNativePath(cfg.Model, false), geminiNativeHeaders(p.apiKey), body)
	details := map[string]interface{}{"status_code": res.statusCode}
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error(), Details: details}
	}
	if !res.ok() {
		msg := upstreamErrorText(res)
		details["error"] = msg
		return DetectorResult{Status: "error", Score: 0, Error: msg, Details: details}
	}

	text := geminiNativeText(res.parsed)
	var parsedAny interface{}
	parseOK := common.UnmarshalJsonStr(text, &parsedAny) == nil
	parsedMap, isMap := parsedAny.(map[string]interface{})
	okJSON := parseOK && isMap
	okSchema := okJSON && parsedMap["ok"] == true && strField(parsedMap, "nonce") == nonce

	finish := geminiNativeFinish(res.parsed)
	score := 0.0
	if okJSON {
		score += 40
	}
	if okSchema {
		score += 50
	}
	if finish == "STOP" || finish == "" {
		score += 10
	}
	markdownSeen := looksLikeMarkdownJSON(text)

	var evaluation string
	switch {
	case okSchema:
		evaluation = "结构化输出正常: 返回内容是纯 JSON,且字段符合 schema。"
	case markdownSeen:
		evaluation = "请求已发送原生 responseSchema,但返回的是普通 Markdown 文本,说明中转站可能没有透传或没有实现 Gemini 结构化输出参数。"
	default:
		evaluation = "请求已发送原生 responseSchema,但返回内容不能按 JSON schema 解析。"
	}

	if isMap {
		details["parsed"] = parsedMap
	} else {
		details["parsed"] = nil
	}
	details["response_text"] = truncate(text, 300)
	details["json_parse"] = okJSON
	details["schema_match"] = okSchema
	details["markdown_json_seen"] = markdownSeen
	details["evaluation_zh"] = evaluation
	details["finish_reason"] = finish

	status := "pass"
	if score < 70 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}
