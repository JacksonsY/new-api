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
// model. opus-4-7 / opus-4-8 (adaptive) 400 on `temperature`; Veridrop strips it
// centrally in client._sanitize_body. Sending it unconditionally would error
// every probe against a *genuine* new-Opus upstream.
func anthropicOmitsTemperature(model string) bool {
	n := normalizeModelID(model)
	return strings.HasPrefix(n, "claude-opus-4-7") || strings.HasPrefix(n, "claude-opus-4-8")
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
	model       string
	text        string
	stopReason  string
	startInput  *int // message_start.message.usage.input_tokens
	deltaOutput *int // message_delta.usage.output_tokens (final cumulative)
	chunkCount  int
}

func parseAnthropicStream(objs []map[string]interface{}) anthropicStream {
	var s anthropicStream
	for _, obj := range objs {
		s.chunkCount++
		switch strField(obj, "type") {
		case "message_start":
			if m := subMap(obj, "message"); m != nil {
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

	var responses []map[string]interface{}
	firstErr := ""
	for i := 0; i < nRuns; i++ {
		res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
			anthropicPayload(cfg.Model, anthropicConsistencyPrompt, 100))
		if res.err != nil {
			if firstErr == "" {
				firstErr = res.err.Error()
			}
			continue
		}
		if !res.ok() {
			if firstErr == "" {
				firstErr = upstreamErrorText(res)
			}
			continue
		}
		responses = append(responses, res.parsed)
	}

	details := map[string]interface{}{"request_model": cfg.Model, "n_runs": nRuns}
	if len(responses) == 0 {
		return DetectorResult{Status: "error", Score: 0, Error: firstErr, Details: details}
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

	outputOK := anthropicOutputSane(shortUsage) && anthropicOutputSane(longUsage)
	sub["output_tokens"] = map[string]interface{}{"pass": outputOK}
	if outputOK {
		score += 15
	}

	streamOK := anthropicStreamUsageClose(shortUsage, stream)
	sub["stream_usage"] = map[string]interface{}{
		"stream_input_tokens":  stream.startInput,
		"stream_output_tokens": stream.deltaOutput,
		"stream_chunk_count":   stream.chunkCount,
		"pass":                 streamOK,
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
		if strField(r, "type") != "message" {
			issues["type_invalid"] = true
		}
		if strField(r, "role") != "assistant" {
			issues["role_invalid"] = true
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
					issues["content_block_unknown_type"] = true
				}
			}
		}
		if sr, ok := r["stop_reason"]; ok {
			s, isStr := sr.(string)
			if sr != nil && (!isStr || !anthropicValidStopReasons[s]) {
				issues["stop_reason_invalid"] = true
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
