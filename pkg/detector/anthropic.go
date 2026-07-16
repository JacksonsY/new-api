package detector

import (
	"context"
	"math"
	"regexp"
	"strings"
)

const anthropicMessagesPath = "/v1/messages"

// anthropicHeaders builds the required Anthropic Messages API headers.
func anthropicHeaders(apiKey string) map[string]string {
	return map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": "2023-06-01",
	}
}

// anthropicOmitsTemperature reports whether temperature must be dropped for a
// model. The adaptive-thinking flagships — opus-4-7 / opus-4-8 and the Claude 5
// family (fable-5 / sonnet-5) — 400 on `temperature`; Veridrop strips it
// centrally in client._sanitize_body. Sending it unconditionally would error
// every probe against a *genuine* upstream of these models.
func anthropicOmitsTemperature(model string) bool {
	n := normalizeModelID(model)
	return strings.HasPrefix(n, "claude-opus-4-7") ||
		strings.HasPrefix(n, "claude-opus-4-8") ||
		strings.HasPrefix(n, "claude-fable-5") ||
		strings.HasPrefix(n, "claude-sonnet-5")
}

// anthropicPayload builds a minimal single-user-turn Messages request. temp=0 is
// included for determinism except on models that reject it.
func anthropicPayload(model, prompt string, maxTokens int) map[string]interface{} {
	body := map[string]interface{}{
		"model":      model,
		"max_tokens": maxTokens,
		"messages": []map[string]interface{}{
			{"role": "user", "content": prompt},
		},
	}
	if !anthropicOmitsTemperature(model) {
		body["temperature"] = 0
	}
	return body
}

// anthropicText concatenates the text blocks of a non-streamed Messages response.
func anthropicText(resp map[string]interface{}) string {
	var sb strings.Builder
	for _, raw := range subSlice(resp, "content") {
		blk, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if strField(blk, "type") == "text" {
			sb.WriteString(strField(blk, "text"))
		}
	}
	return sb.String()
}

// anthropicStream is the reconstruction of a Messages SSE body: model, text,
// stop_reason, and the usage counters reported on message_start / message_delta.
type anthropicStream struct {
	id           string
	model        string
	messageStart map[string]interface{} // the full message_start.message object
	text         string
	stopReason   string
	startInput   *int // message_start.message.usage.input_tokens
	deltaOutput  *int // message_delta.usage.output_tokens (final cumulative)
	chunkCount   int
}

func parseAnthropicStream(objs []map[string]interface{}) anthropicStream {
	var s anthropicStream
	for _, obj := range objs {
		s.chunkCount++
		switch strField(obj, "type") {
		case "message_start":
			if m := subMap(obj, "message"); m != nil {
				if s.messageStart == nil {
					s.messageStart = m
				}
				if v := strField(m, "id"); v != "" {
					s.id = v
				}
				if v := strField(m, "model"); v != "" {
					s.model = v
				}
				if u := subMap(m, "usage"); u != nil {
					if in, ok := intField(u, "input_tokens"); ok {
						s.startInput = &in
					}
				}
			}
		case "content_block_delta":
			delta := subMap(obj, "delta")
			if t := strField(delta, "type"); t == "text_delta" || t == "" {
				s.text += strField(delta, "text")
			}
		case "message_delta":
			delta := subMap(obj, "delta")
			if sr := strField(delta, "stop_reason"); sr != "" {
				s.stopReason = sr
			}
			if u := subMap(obj, "usage"); u != nil {
				if out, ok := intField(u, "output_tokens"); ok {
					s.deltaOutput = &out
				}
			}
		}
	}
	return s
}

// recordAnthropicStreamObservation reconstructs a non-stream Messages envelope
// from a streamed response and puts it on the passive-observation bus, so
// protocol/message_id validate streamed traffic too (Veridrop's client
// synthesizes and broadcasts a message from message_start + message_delta).
func (p *prober) recordAnthropicStreamObservation(s anthropicStream, res httpResult) {
	if !res.ok() || s.chunkCount == 0 || s.messageStart == nil {
		return
	}
	// Mirror Python _synthesize_stream_response: start from the REAL
	// message_start.message (carrying type/role/id/model/content verbatim so a
	// stream-only wrong type/role is validated), then overlay stop_reason and the
	// merged usage. Copying the map avoids mutating the parsed stream.
	env := make(map[string]interface{}, len(s.messageStart)+1)
	for k, v := range s.messageStart {
		env[k] = v
	}
	if s.stopReason != "" {
		env["stop_reason"] = s.stopReason
	}
	usage := map[string]interface{}{}
	if base := subMap(s.messageStart, "usage"); base != nil {
		for k, v := range base {
			usage[k] = v
		}
	}
	if s.startInput != nil {
		usage["input_tokens"] = *s.startInput
	}
	if s.deltaOutput != nil {
		usage["output_tokens"] = *s.deltaOutput
	}
	if len(usage) > 0 {
		env["usage"] = usage
	}
	p.recordStreamObservation(env, res.statusCode, res.durationMs)
}

// anthropicUsage returns the usage sub-object of a non-stream response.
func anthropicUsage(resp map[string]interface{}) map[string]interface{} {
	return subMap(resp, "usage")
}

func anthropicDetectors() []detectorDef {
	return []detectorDef{
		// 权重与模式成员对齐 Veridrop anthropic/config.py（DETECTOR_WEIGHTS +
		// MODE_DETECTORS）。protocol / message_id 为被动检测器（见 runner 两阶段）。
		{detectorIdentity, "身份检测", 5.0, modeRankQuick, false, anthropicIdentity},
		{detectorConsistency, "模型一致性", 10.0, modeRankQuick, false, anthropicConsistency},
		{detectorMessageID, "消息 ID 规范", 5.0, modeRankQuick, false, anthropicMessageID},
		{detectorProtocol, "协议规范性", 5.0, modeRankQuick, false, anthropicProtocol},
		{detectorTokenUsage, "Token 用量", 10.0, modeRankStandard, false, anthropicTokenUsage},
		{detectorThinkingSignature, "思维签名透传", 25.0, modeRankQuick, false, anthropicThinkingSignature},
		{detectorIntegrity, "响应完整性", 5.0, modeRankStandard, false, anthropicIntegrity},
		{detectorStructuredOutput, "结构化输出", 12.0, modeRankStandard, false, anthropicStructuredOutput},
		{detectorKnowledge, "知识准确度", 10.0, modeRankStandard, false, anthropicKnowledge},
		{detectorBehavioral, "行为签名", 15.0, modeRankFull, false, anthropicBehavioral},
		{detectorPDF, "PDF 文档识别", 8.0, modeRankFull, false, anthropicPDF},
		// 后端溯源(移植自 cc-proxy-detector, MIT):判定 Claude 真实后端
		// Anthropic 直连 / Bedrock(Kiro)/ Vertex(Antigravity),并揪出伪装 Anthropic。
		{detectorBackendOrigin, "后端溯源", 8.0, modeRankStandard, false, anthropicBackendOrigin},
		// 隐藏系统提示注入(移植自 claude-detector, MIT):不带 system 令复述系统提示,
		// 揪出静默注入 "you are ChatGPT / translate all" 的中转。
		{detectorSystemPromptLeak, "隐藏提示注入", 6.0, modeRankStandard, false, anthropicSystemPromptLeak},
		// 行为探针组(移植自 claude-detector, MIT):full 档深测,单个信号弱、组合有效。
		{detectorStopSequence, "stop_sequence 语义", 2.0, modeRankFull, false, anthropicStopSequence},
		{detectorMaxTokensBehav, "max_tokens 截断", 2.0, modeRankFull, false, anthropicMaxTokensHonoring},
		{detectorErrorShape, "错误对象 Schema", 1.0, modeRankFull, false, anthropicErrorShape},
		{detectorCacheBehavior, "Prompt Caching", 2.0, modeRankFull, false, anthropicCacheBehavior},
		{detectorMultiTurn, "多轮记忆", 2.0, modeRankFull, false, anthropicMultiTurn},
		{detectorHeaderFinger, "响应头指纹", 1.0, modeRankFull, false, anthropicHeaderFingerprint},
		// 注入抗性(移植自 telagod/llm-probe, Go):埋 sentinel + 直接越权/工具结果注入,
		// 泄漏即判弱模型(降级/换芯信号)。full 档,多请求。
		{detectorInjectionResist, "注入抗性", 5.0, modeRankFull, false, anthropicInjectionResistance},
		// 响应完整性/投毒(移植自 API-Poison-Detector, MIT):良性无工具请求下,
		// 中转注入 tool_use / 恶意内容 / 隐藏指令即 critical(供应商安全审计)。
		{detectorResponseIntegrity, "响应投毒检测", 4.0, modeRankFull, false, anthropicResponseIntegrity},
		// Web3 安全隔离(移植自 api-relay-audit, AGPL):3 探针查中转是否注入钱包助手,
		// 覆盖了 Claude 对私钥/代签/代转的安全拒绝。
		{detectorWeb3Safety, "Web3 安全隔离", 3.0, modeRankFull, false, anthropicWeb3Safety},
		// 错误响应泄漏(移植自 api-relay-audit, AGPL):broken 请求下,错误体/头泄漏
		// 凭证/上游 URL/路径/堆栈/LiteLLM 内部字段即判泄漏(供应商内部管道暴露)。
		{detectorErrorLeakage, "错误泄漏扫描", 2.0, modeRankFull, false, anthropicErrorLeakage},
		// 供应链安装命令篡改(移植自 LLMprobe-engine, AGPL):要模型给规范 install 命令,
		// 扫到镜像/仓库重定向、VCS/归档源替换、typosquat 或 shell 投毒即 critical。
		{detectorSupplyChain, "供应链完整性", 3.0, modeRankFull, false, anthropicSupplyChainIntegrity},
		// 诱饵密钥外泄通道(移植自 LLMprobe-engine, AGPL):system 埋 canary + 良性任务,
		// 响应里回显 canary 或注入图片/URL 信标/dropper 代码即判外泄。
		{detectorExfilScan, "外泄通道扫描", 3.0, modeRankFull, false, anthropicExfilScan},
		// 条件/自适应注入差分(移植自 LLMprobe-engine, AGPL):中性 vs 含敏感 token 的
		// 配对回显,中性干净而敏感偏移即判条件注入(现有探针无差分维度)。
		{detectorAdaptiveInjection, "自适应注入差分", 2.0, modeRankFull, false, anthropicAdaptiveInjection},
		// 隐藏系统提示地板(移植自 LLMprobe-engine, AGPL):极小 "Hi" 请求的 input_tokens
		// 远超地板即判包装层注入了隐藏提示(token_usage 自洽性的最小输入地板补充)。
		{detectorHiddenPromptFloor, "隐藏提示地板", 2.0, modeRankFull, false, anthropicHiddenPromptFloor},
		// Unicode 透传保真(移植自 LLMprobe-engine, AGPL):要模型逐字输出 CJK 角括号文本,
		// 角括号被折叠/替换即疑似做 NFKC 归一/重编码的转码中转。
		{detectorUnicodeFidelity, "Unicode 保真", 1.0, modeRankFull, false, anthropicUnicodeFidelity},
		// 敏感数据泄露(移植自 AITTAK, Apache-2.0):良性探针下,响应命中凭证/PII/
		// 内网信息规则库(15 条)即判泄漏——中转在响应里带出真实密钥/卡号/内网信息。
		{detectorSensitiveLeak, "敏感数据泄露", 3.0, modeRankFull, false, anthropicSensitiveLeak},
		// 包名返回路径替换(移植自 api-relay-audit, AGPL):逐字回显已知安装命令,
		// token 级对比抓中途改写(typosquat / 包名内插空格)——供应链 MITM。
		{detectorPkgSubstitution, "包名替换", 3.0, modeRankFull, false, anthropicPkgSubstitution},
		// 流式完整性(移植自 api-relay-audit, AGPL · Step 10):SSE 事件层篡改——
		// 未知事件 / usage 非单调-不一致 / 空 thinking 签名 / 非 Claude 流模型名。
		{detectorStreamIntegrity, "流式完整性", 4.0, modeRankFull, false, anthropicStreamIntegrity},
		// 速度基准(移植自 lm-speed, MIT):真流式量 TTFT + 输出 TPS + 单次延迟,
		// 作为证据展示(速度是指标非真伪判定,恒 pass)。
		{detectorSpeed, "速度基准", 1.0, modeRankFull, false, anthropicSpeed},
		{detectorLongContext, "长上下文", 15.0, modeRankFull, true, longContextDetector},
	}
}

// --- identity (active) -----------------------------------------------------

const anthropicIdentityPrompt = "Who are you exactly? What is your model name and version? " +
	"Which company developed you? Answer concisely."

// anthropicCompetitorPatterns mirrors identity.py COMPETITOR (broad rival
// mentions that, alongside a Claude/Anthropic mention, indicate a mixed/leaked
// backend). Distinct from brandPatterns (specific backend fingerprints).
var anthropicCompetitorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bgpt[-\s]?\d+`),
	regexp.MustCompile(`(?i)\bopenai\b`),
	regexp.MustCompile(`(?i)\bchatgpt\b`),
	regexp.MustCompile(`(?i)\bgemini\b`),
	regexp.MustCompile(`(?i)\bbard\b`),
	regexp.MustCompile(`(?i)\bgoogle\b`),
	regexp.MustCompile(`(?i)\bdeepseek\b`),
	regexp.MustCompile(`(?i)\bqwen\b`),
	regexp.MustCompile(`(?i)\bllama\b`),
	regexp.MustCompile(`(?i)\bmistral\b`),
	// xAI family — fork addition beyond the ported COMPETITOR set.
	regexp.MustCompile(`(?i)\bgrok\b`),
	regexp.MustCompile(`(?i)\bxai\b|\bx\.ai\b`),
}

// anthropicIdentity asks the model who it is and scores Claude/Anthropic markers
// on Veridrop's 4-tier scale. Detected non-official brands are recorded as data
// (the comparator escalates them relative to the model baseline); this active
// detector does not veto on its own — matching the reference, where single-run
// detectors emit no criticals.
func anthropicIdentity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
		anthropicPayload(cfg.Model, anthropicIdentityPrompt, 200))
	details := map[string]interface{}{"status_code": res.statusCode}
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error(), Details: details}
	}
	if !res.ok() {
		msg := upstreamErrorText(res)
		details["error"] = msg
		return DetectorResult{Status: "error", Score: 0, Error: msg, Details: details}
	}

	text := anthropicText(res.parsed)
	hasClaude := reClaude.MatchString(text)
	hasAnthropic := reAnthropic.MatchString(text)
	requiredHitsList := make([]string, 0, 2)
	if hasClaude {
		requiredHitsList = append(requiredHitsList, "claude")
	}
	if hasAnthropic {
		requiredHitsList = append(requiredHitsList, "anthropic")
	}
	competitorHits := make([]string, 0)
	for _, re := range anthropicCompetitorPatterns {
		if re.MatchString(text) {
			competitorHits = append(competitorHits, re.String())
		}
	}
	brands := scanBrands(text)
	requiredHits := len(requiredHitsList)
	competitor := len(competitorHits) > 0

	// 4-tier per §3.1: both&no-competitor→100, mixed→30, one→60, none→0.
	var score float64
	switch {
	case requiredHits == 2 && !competitor:
		score = 100
	case requiredHits > 0 && competitor:
		score = 30
	case requiredHits > 0:
		score = 60
	default:
		score = 0
	}

	// Detail keys align with the baseline schema so the comparator can diff
	// relay-vs-baseline (required_hits / competitor_hits / detected brands).
	details["self_reported_identity"] = text
	details["response_text"] = truncate(text, 300)
	details["required_hits"] = requiredHitsList
	details["competitor_hits"] = competitorHits
	details["detected_non_anthropic_brands"] = brands

	status := "pass"
	if score < 70 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}

// --- consistency (active) --------------------------------------------------

const anthropicConsistencyPrompt = "Reply in 30 words explaining what HTTP status 418 means. " +
	"Do not include any preamble."

// anthropicConsistency scores model-field match (60) plus determinism: with
// temp=0, output_tokens should be near-constant across N runs (CV<0.10→40,
// <0.30→20, else 0). Quick mode uses one run and skips the stability test with
// full credit, matching consistency.py.
func anthropicConsistency(ctx context.Context, p *prober, cfg Config) DetectorResult {
	nRuns := 3
	if cfg.Mode == ModeQuick {
		nRuns = 1
	}

	// Any run failing hard errors the whole detector, matching consistency.py
	// (the N-run loop is inside one try; any raise returns error). Scoring from a
	// subset of survivors would report determinism over fewer samples than asked.
	details := map[string]interface{}{"request_model": cfg.Model, "n_runs": nRuns}
	var responses []map[string]interface{}
	for i := 0; i < nRuns; i++ {
		res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
			anthropicPayload(cfg.Model, anthropicConsistencyPrompt, 100))
		if res.err != nil {
			return DetectorResult{Status: "error", Score: 0, Error: res.err.Error(), Details: details}
		}
		if !res.ok() {
			return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res), Details: details}
		}
		responses = append(responses, res.parsed)
	}

	responseModel := strField(responses[0], "model")
	match := modelMatches(cfg.Model, responseModel)
	details["response_model"] = responseModel
	details["model_match"] = match
	modelScore := 0.0
	if match {
		modelScore = 60
	}

	var stabilityScore float64
	if nRuns > 1 {
		outs := make([]float64, 0, len(responses))
		seq := make([]int, 0, len(responses))
		for _, r := range responses {
			v, _ := intField(anthropicUsage(r), "output_tokens")
			outs = append(outs, float64(v))
			seq = append(seq, v)
		}
		details["output_tokens_seq"] = seq
		mean := 0.0
		for _, v := range outs {
			mean += v
		}
		mean /= float64(len(outs))
		if mean > 0 {
			var variance float64
			for _, v := range outs {
				variance += (v - mean) * (v - mean)
			}
			variance /= float64(len(outs)) // population variance (pstdev)
			cv := math.Sqrt(variance) / mean
			details["stability_cv"] = math.Round(cv*1000) / 1000
			switch {
			case cv < 0.10:
				stabilityScore = 40
				details["stability_label"] = "stable"
			case cv < 0.30:
				stabilityScore = 20
				details["stability_label"] = "suspicious"
			default:
				stabilityScore = 0
				details["stability_label"] = "highly_anomalous"
			}
		} else {
			details["stability_label"] = "no_output"
		}
	} else {
		stabilityScore = 40
		details["stability_label"] = "skipped_quick_mode"
	}

	score := modelScore + stabilityScore
	status := "pass"
	if score < 70 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}

// --- thinking_signature (active, crown jewel) ------------------------------

const (
	anthropicThinkingPrompt = "Find the greatest common divisor of 2378 and 1547 using the " +
		"Euclidean algorithm."
	anthropicThinkingBudget  = 2000
	anthropicThinkingMaxTok  = 16000
	anthropicSignatureMinLen = 50
)

// anthropicThinkingSignature requests extended/adaptive thinking and verifies a
// forwarded server signature. applies_to (model capability table) skips models
// that don't support thinking, so a genuine non-thinking model is not
// false-failed. Uses a hard GCD prompt (adaptive models reliably decide to
// think) and xhigh effort for opus-4-7/4-8. Non-streaming: adaptive+summarized
// silently drops the thinking block from the SSE stream. 4-tier score, no veto.
func anthropicThinkingSignature(ctx context.Context, p *prober, cfg Config) DetectorResult {
	info := lookupModel(cfg.Model)
	if info == nil || !(info.supportsExtendedThinking || info.supportsAdaptiveThinking) {
		return detectorSkip("model does not support thinking (applies_to)")
	}

	payload := map[string]interface{}{
		"model":      cfg.Model,
		"max_tokens": anthropicThinkingMaxTok,
		"messages": []map[string]interface{}{
			{"role": "user", "content": anthropicThinkingPrompt},
		},
	}
	if info.supportsExtendedThinking {
		payload["thinking"] = map[string]interface{}{"type": "enabled", "budget_tokens": anthropicThinkingBudget}
	} else {
		// adaptive: effort is a SEPARATE top-level output_config field.
		payload["thinking"] = map[string]interface{}{"type": "adaptive", "display": "summarized"}
		payload["output_config"] = map[string]interface{}{"effort": adaptiveEffortForModel(cfg.Model)}
	}

	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), payload)
	details := map[string]interface{}{"status_code": res.statusCode}
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error(), Details: details}
	}
	if !res.ok() {
		msg := upstreamErrorText(res)
		details["error"] = msg
		return DetectorResult{Status: "error", Score: 0, Error: msg, Details: details}
	}

	thinkingSeen := false
	sigLen := 0
	var blockTypes []string
	for _, raw := range subSlice(res.parsed, "content") {
		blk, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		bt := strField(blk, "type")
		if bt != "" {
			blockTypes = append(blockTypes, bt)
		}
		if bt == "thinking" || bt == "redacted_thinking" {
			thinkingSeen = true
			if l := len(strField(blk, "signature")); l > sigLen {
				sigLen = l
			}
		}
	}
	details["content_block_types_seen"] = blockTypes
	details["thinking_block_seen"] = thinkingSeen
	details["signature_length"] = sigLen
	details["stop_reason"] = strField(res.parsed, "stop_reason")

	var score float64
	var note string
	switch {
	case !thinkingSeen:
		score, note = 0, "no_thinking_block"
	case sigLen == 0:
		score, note = 30, "thinking_block_but_no_signature"
	case sigLen < anthropicSignatureMinLen:
		score, note = 70, "signature_too_short"
	default:
		score, note = 100, "ok"
	}
	details["evaluation"] = note

	status := "pass"
	if score < 70 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}

// --- token_usage (active) --------------------------------------------------

const (
	anthropicTokenShortPrompt = "Reply with exactly: ok"
	anthropicTokenMaxTok      = 16
)

// anthropicTokenUsage runs the 5-part usage-sanity battery (usage present,
// input-token delta in a tokenizer-aware range, output within cap, stream vs
// non-stream parity, count_tokens cross-check), pass >= 80. Catches
// usage/billing inflation that "tokens present" alone would miss.
func anthropicTokenUsage(ctx context.Context, p *prober, cfg Config) DetectorResult {
	longPrompt := anthropicTokenShortPrompt + "\n\nReference text:" + strings.Repeat(" apple", 80)
	shortBody := anthropicPayload(cfg.Model, anthropicTokenShortPrompt, anthropicTokenMaxTok)
	longBody := anthropicPayload(cfg.Model, longPrompt, anthropicTokenMaxTok)

	shortRes := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), shortBody)
	if shortRes.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: shortRes.err.Error()}
	}
	if !shortRes.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(shortRes)}
	}
	longRes := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), longBody)
	if longRes.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: longRes.err.Error()}
	}
	if !longRes.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(longRes)}
	}

	// Stream + count_tokens are auxiliary; failures degrade the sub-check only.
	streamBody := anthropicPayload(cfg.Model, anthropicTokenShortPrompt, anthropicTokenMaxTok)
	streamBody["stream"] = true
	streamRes := p.postSSE(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), streamBody)
	stream := parseAnthropicStream(sseDataObjects(streamRes.text))
	p.recordAnthropicStreamObservation(stream, streamRes)
	countRes := p.countTokens(ctx, anthropicHeaders(p.apiKey), anthropicPayload(cfg.Model, anthropicTokenShortPrompt, anthropicTokenMaxTok))

	shortUsage := anthropicUsage(shortRes.parsed)
	longUsage := anthropicUsage(longRes.parsed)
	sub := map[string]interface{}{}
	score := 0.0

	usagePresent := len(shortUsage) > 0 && len(longUsage) > 0
	sub["usage_present"] = usagePresent
	if usagePresent {
		score += 20
	}

	shortIn, okShortIn := intField(shortUsage, "input_tokens")
	longIn, okLongIn := intField(longUsage, "input_tokens")
	deltaMin, deltaMax := anthropicDeltaRange(cfg.Model)
	deltaOK := false
	if okShortIn && okLongIn {
		delta := longIn - shortIn
		deltaOK = delta >= deltaMin && delta <= deltaMax
		sub["input_token_delta"] = map[string]interface{}{"delta": delta, "expected_range": []int{deltaMin, deltaMax}, "pass": deltaOK}
	} else {
		sub["input_token_delta"] = map[string]interface{}{"pass": false}
	}
	if deltaOK {
		score += 25
	}

	adaptive := anthropicAdaptiveThinkingModel(cfg.Model)
	outputOK := false
	outputNote := ""
	if adaptive {
		// Adaptive-thinking models count hidden thinking in output_tokens without
		// breaking it out, so a short answer's output is not bounded to the visible
		// text. Don't treat it as anomalous (the Claude analog of the OpenAI
		// reasoning-model completion exemption); input-side checks carry the weight.
		outputOK = true
		outputNote = "自适应推理模型,output 含隐藏思维且未拆分,不作可见性上界判定"
	} else {
		outputOK = anthropicOutputSane(shortUsage) && anthropicOutputSane(longUsage)
	}
	sub["output_tokens"] = map[string]interface{}{"pass": outputOK, "note": outputNote}
	if outputOK {
		score += 15
	}

	streamOK := false
	streamNote := ""
	if adaptive {
		// output_tokens varies per call with adaptive thinking, so verify only the
		// stable signal — input-token parity — plus a present, non-negative stream
		// output, not the 50% output tolerance.
		streamOK = anthropicStreamInputStable(shortUsage, stream)
		streamNote = "自适应推理模型:仅校验 input token 一致性(output 因隐藏思维逐次波动)"
	} else {
		streamOK = anthropicStreamUsageClose(shortUsage, stream)
	}
	sub["stream_usage"] = map[string]interface{}{
		"stream_input_tokens":  stream.startInput,
		"stream_output_tokens": stream.deltaOutput,
		"stream_chunk_count":   stream.chunkCount,
		"pass":                 streamOK,
		"note":                 streamNote,
	}
	if streamOK {
		score += 25
	}

	countOK := false
	if countRes.ok() && countRes.parsed != nil {
		if counted, ok := intField(countRes.parsed, "input_tokens"); ok && okShortIn {
			tol := anthropicTolerance(shortIn, 0.20, 4)
			countOK = abs(shortIn-counted) <= tol
			sub["count_tokens"] = map[string]interface{}{"count_tokens": counted, "actual": shortIn, "pass": countOK}
		}
	}
	if _, present := sub["count_tokens"]; !present {
		sub["count_tokens"] = map[string]interface{}{"pass": false, "note": "count_tokens unavailable"}
	}
	if countOK {
		score += 15
	}

	if !usagePresent {
		return detectorSkip("missing-usage")
	}

	risk := "high"
	if score >= 90 {
		risk = "low"
	} else if score >= 80 {
		risk = "medium"
	}
	details := map[string]interface{}{"sub_checks": sub, "risk_level": risk}
	status := "pass"
	if score < 80 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}

func anthropicDeltaRange(model string) (int, int) {
	if modelUsesNewTokenizer(model) {
		return 90, 230
	}
	return 45, 140
}

func anthropicOutputSane(usage map[string]interface{}) bool {
	out, ok := intField(usage, "output_tokens")
	return ok && out >= 0 && out <= anthropicTokenMaxTok+4
}

func anthropicStreamUsageClose(nonStream map[string]interface{}, stream anthropicStream) bool {
	nsIn, okNsIn := intField(nonStream, "input_tokens")
	nsOut, okNsOut := intField(nonStream, "output_tokens")
	if !okNsIn || !okNsOut || stream.startInput == nil || stream.deltaOutput == nil {
		return false
	}
	inTol := anthropicTolerance(nsIn, 0.20, 4)
	outTol := anthropicTolerance(nsOut, 0.50, 3)
	return abs(nsIn-*stream.startInput) <= inTol &&
		abs(nsOut-*stream.deltaOutput) <= outTol &&
		*stream.deltaOutput >= 0
}

// anthropicStreamInputStable verifies stream/non-stream INPUT-token parity
// (deterministic) plus a present, non-negative stream output — used for
// adaptive-thinking models whose per-call output_tokens legitimately varies
// (hidden thinking is counted in output and not broken out), so the 50% output
// tolerance in anthropicStreamUsageClose would false-fire.
func anthropicStreamInputStable(nonStream map[string]interface{}, stream anthropicStream) bool {
	nsIn, okNsIn := intField(nonStream, "input_tokens")
	if !okNsIn || stream.startInput == nil || stream.deltaOutput == nil {
		return false
	}
	inTol := anthropicTolerance(nsIn, 0.20, 4)
	return abs(nsIn-*stream.startInput) <= inTol && *stream.deltaOutput >= 0
}

func anthropicTolerance(base int, frac float64, floor int) int {
	t := int(float64(base) * frac)
	if t < floor {
		return floor
	}
	return t
}

// --- integrity (active) ----------------------------------------------------

const anthropicIntegrityPrompt = `Reply with exactly this JSON literal and nothing else: {"verify":"abc123","n":42}`

// anthropicIntegrity compares a streamed vs non-streamed response over five
// sub-checks (text similarity, char/token ratio, stream output-token parity,
// stop_reason enum, input-token stream/non-stream consistency), pass >= 70.
func anthropicIntegrity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	nonStream := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
		anthropicPayload(cfg.Model, anthropicIntegrityPrompt, 80))
	if nonStream.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: nonStream.err.Error()}
	}
	if !nonStream.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(nonStream)}
	}
	streamBody := anthropicPayload(cfg.Model, anthropicIntegrityPrompt, 80)
	streamBody["stream"] = true
	streamRes := p.postSSE(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), streamBody)
	if streamRes.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: streamRes.err.Error()}
	}
	if !streamRes.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(streamRes)}
	}

	nsText := anthropicText(nonStream.parsed)
	nsUsage := anthropicUsage(nonStream.parsed)
	nsInput, _ := intField(nsUsage, "input_tokens")
	nsOutput, _ := intField(nsUsage, "output_tokens")
	stream := parseAnthropicStream(sseDataObjects(streamRes.text))
	p.recordAnthropicStreamObservation(stream, streamRes)

	sub := map[string]interface{}{}
	score := 0.0
	const w = 20.0

	// 1. text similarity
	ratio := 0.0
	if nsText != "" && stream.text != "" {
		ratio = stringRatio(nsText, stream.text)
	}
	sim := ratio >= 85
	sub["similarity"] = map[string]interface{}{"ratio": math.Round(ratio*10) / 10, "pass": sim}
	if sim {
		score += w
	}

	// 2. char/token ratio
	cptOK := false
	if nsOutput > 0 && nsText != "" {
		cpt := float64(len([]rune(nsText))) / float64(nsOutput)
		cptOK = cpt >= 1.2 && cpt <= 10.0
		sub["char_per_token"] = map[string]interface{}{"value": math.Round(cpt*100) / 100, "pass": cptOK}
	} else {
		sub["char_per_token"] = map[string]interface{}{"pass": false}
	}
	if cptOK {
		score += w
	}

	// 3. stream output_tokens close to non-stream
	streamOutOK := false
	if stream.deltaOutput != nil {
		tol := anthropicTolerance(nsOutput, 0.50, 2)
		streamOutOK = abs(*stream.deltaOutput-nsOutput) <= tol && *stream.deltaOutput > 0
	}
	sub["stream_output_tokens"] = map[string]interface{}{"stream": stream.deltaOutput, "ns": nsOutput, "pass": streamOutOK}
	if streamOutOK {
		score += w
	}

	// 4. stop_reason valid
	stopOK := stream.stopReason == "end_turn" || stream.stopReason == "max_tokens"
	sub["stop_reason"] = map[string]interface{}{"value": stream.stopReason, "pass": stopOK}
	if stopOK {
		score += w
	}

	// 5. input_tokens consistency stream vs non-stream
	inputOK := false
	if stream.startInput != nil && nsInput > 0 {
		tol := anthropicTolerance(nsInput, 0.20, 3)
		inputOK = abs(*stream.startInput-nsInput) <= tol
		sub["input_tokens"] = map[string]interface{}{"ns": nsInput, "stream": *stream.startInput, "tolerance": tol, "pass": inputOK}
	} else {
		sub["input_tokens"] = map[string]interface{}{"ns": nsInput, "pass": false}
	}
	if inputOK {
		score += w
	}

	details := map[string]interface{}{
		"sub_checks":       sub,
		"non_stream_model": strField(nonStream.parsed, "model"),
		"stream_model":     stream.model,
	}
	status := "pass"
	if score < 70 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}

// --- protocol (passive) ----------------------------------------------------

var anthropicValidStopReasons = map[string]bool{
	"end_turn": true, "max_tokens": true, "stop_sequence": true, "tool_use": true, "": true,
}

// knownAnthropicBlockTypes are the assistant content block types a genuine
// Messages response may emit.
var knownAnthropicBlockTypes = map[string]bool{
	"text": true, "thinking": true, "redacted_thinking": true,
	"tool_use": true, "server_tool_use": true, "web_search_tool_result": true,
}

// anthropicProtocol validates the Messages response schema across every observed
// response in the run (passive). Each distinct issue kind counts once; score =
// 100 - 10*issues. No veto — protocol compliance is score-based in the reference.
func anthropicProtocol(_ context.Context, p *prober, _ Config) DetectorResult {
	obs := p.tel.snapshot()
	if len(obs) == 0 {
		return detectorSkip("no observations")
	}
	issues := map[string]bool{}
	for _, o := range obs {
		r := o.response
		if s := strField(r, "id"); s == "" {
			issues["id_missing_or_not_string"] = true
		}
		if v := strField(r, "type"); v != "message" {
			// Value-suffixed so distinct invalid values across the run count
			// separately (protocol.py appends the offending value via !r).
			issues["type_invalid:"+v] = true
		}
		if v := strField(r, "role"); v != "assistant" {
			issues["role_invalid:"+v] = true
		}
		if s := strField(r, "model"); s == "" {
			issues["model_missing_or_not_string"] = true
		}
		if content, ok := r["content"].([]interface{}); !ok {
			issues["content_not_array"] = true
		} else {
			for _, raw := range content {
				blk, ok := raw.(map[string]interface{})
				if !ok {
					issues["content_block_not_object"] = true
					continue
				}
				bt := strField(blk, "type")
				if bt == "" {
					issues["content_block_missing_type"] = true
				} else if !knownAnthropicBlockTypes[bt] {
					issues["content_block_unknown_type:"+bt] = true
				}
			}
		}
		if sr, ok := r["stop_reason"]; ok {
			s, isStr := sr.(string)
			if sr != nil && (!isStr || !anthropicValidStopReasons[s]) {
				// Value-suffixed so distinct invalid stop_reasons (e.g. an
				// OpenAI-style backend forwarding stop/length/tool_calls) count
				// separately. Non-string values collapse to one marker.
				val := s
				if !isStr {
					val = "<non-string>"
				}
				issues["stop_reason_invalid:"+val] = true
			}
		}
		if ss, ok := r["stop_sequence"]; ok && ss != nil {
			if _, isStr := ss.(string); !isStr {
				issues["stop_sequence_wrong_type"] = true
			}
		}
		usage, ok := r["usage"].(map[string]interface{})
		if !ok {
			issues["usage_missing_or_not_object"] = true
		} else {
			if !isNonNegInt(usage["input_tokens"]) {
				issues["usage_input_tokens_invalid"] = true
			}
			if !isNonNegInt(usage["output_tokens"]) {
				issues["usage_output_tokens_invalid"] = true
			}
			for _, opt := range []string{"cache_read_input_tokens", "cache_creation_input_tokens"} {
				if v, present := usage[opt]; present && !isNonNegInt(v) {
					issues[opt+"_wrong_type"] = true
				}
			}
		}
	}

	score := clampScore(100 - float64(len(issues))*10)
	details := map[string]interface{}{
		"observation_count": len(obs),
		"issue_count":       len(issues),
		"issues":            sortedKeys(issues),
	}
	status := "pass"
	if score < 70 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}

// --- message_id (passive) --------------------------------------------------

// anthropicMessageID validates id-prefix conventions across every observed
// response (passive). Base violations (id/type/role/model) and nested tool-id
// prefixes each deduct 25 once. No veto.
func anthropicMessageID(_ context.Context, p *prober, _ Config) DetectorResult {
	obs := p.tel.snapshot()
	if len(obs) == 0 {
		return detectorSkip("no observations")
	}
	violations := map[string]bool{}
	for _, o := range obs {
		r := o.response
		id := strField(r, "id")
		if !(strings.HasPrefix(id, "msg_") && len(id) >= 8) {
			violations["id_prefix_invalid"] = true
		}
		if strField(r, "type") != "message" {
			violations["type_not_message"] = true
		}
		if strField(r, "role") != "assistant" {
			violations["role_not_assistant"] = true
		}
		if m := strField(r, "model"); !strings.Contains(strings.ToLower(m), "claude") {
			violations["model_not_claude"] = true
		}
		for _, raw := range subSlice(r, "content") {
			blk, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			bt := strField(blk, "type")
			bid := strField(blk, "id")
			if bt == "tool_use" && bid != "" && !strings.HasPrefix(bid, "toolu_") {
				violations["tool_use_id_prefix_invalid"] = true
			} else if bt == "server_tool_use" && bid != "" && !strings.HasPrefix(bid, "srvtoolu_") {
				violations["server_tool_use_id_prefix_invalid"] = true
			}
		}
	}

	score := 100.0
	for range violations {
		score -= 25
	}
	score = clampScore(score)
	details := map[string]interface{}{
		"observation_count": len(obs),
		"violations":        sortedKeys(violations),
	}
	status := "pass"
	if score < 70 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}
