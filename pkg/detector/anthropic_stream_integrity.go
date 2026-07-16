package detector

// anthropic_stream_integrity.go ports Step 10 (SSE-level stream integrity) of
// api-relay-audit (github.com/toby-bridges/api-relay-audit, AGPL-3.0,
// api_relay_audit/stream_integrity.py). It catches a relay that rewrites/proxies
// Claude's streaming responses at the SSE event layer even when the final text
// looks correct: an unknown SSE event type (not one of Anthropic's 7), a usage
// field rewritten non-monotonically or inconsistently (under-billing / hidden
// downgrade), an empty thinking signature_delta (thinking-model downgrade), or a
// non-Claude message_start model. Distinct from the integrity detector, which
// compares stream vs non-stream TEXT — this inspects the event STRUCTURE.
// Attribution retained per the AGPL; additive, does not replace any new-api /
// QuantumNous identity.

import (
	"context"
	"strings"
)

// knownSSEEventTypes is Anthropic's 7-event stream schema; anything else is an
// injected/rewritten event.
var knownSSEEventTypes = map[string]bool{
	"ping": true, "message_start": true, "content_block_start": true,
	"content_block_delta": true, "content_block_stop": true,
	"message_delta": true, "message_stop": true,
}

const maxUnknownEventsReported = 5

type anthropicStreamSignals struct {
	eventTypes               []string
	inputTokens              *int
	outputTokensSamples      []int
	messageDeltaInputSamples []int
	emptySignatureDeltas     int
	messageStartModel        string
	hasMessageStart          bool
	hasContentBlockStart     bool
	hasContentBlockDelta     bool
	hasMessageDelta          bool
	hasMessageStop           bool
	hasTextDelta             bool
}

// collectAnthropicStreamSignals extracts the SSE-layer signals from the parsed
// data objects of an Anthropic stream (each object's "type" is its event type).
func collectAnthropicStreamSignals(objs []map[string]interface{}) anthropicStreamSignals {
	var sig anthropicStreamSignals
	for _, obj := range objs {
		et := strField(obj, "type")
		sig.eventTypes = append(sig.eventTypes, et)
		switch et {
		case "message_start":
			sig.hasMessageStart = true
			msg := subMap(obj, "message")
			sig.messageStartModel = strField(msg, "model")
			if v, ok := intField(subMap(msg, "usage"), "input_tokens"); ok {
				sig.inputTokens = &v
			}
		case "content_block_start":
			sig.hasContentBlockStart = true
		case "content_block_delta":
			sig.hasContentBlockDelta = true
			delta := subMap(obj, "delta")
			switch strField(delta, "type") {
			case "text_delta":
				sig.hasTextDelta = true
			case "signature_delta":
				if strings.TrimSpace(strField(delta, "signature")) == "" {
					sig.emptySignatureDeltas++
				}
			}
		case "message_delta":
			sig.hasMessageDelta = true
			usage := subMap(obj, "usage")
			if v, ok := intField(usage, "output_tokens"); ok {
				sig.outputTokensSamples = append(sig.outputTokensSamples, v)
			}
			if v, ok := intField(usage, "input_tokens"); ok {
				sig.messageDeltaInputSamples = append(sig.messageDeltaInputSamples, v)
			}
		case "message_stop":
			sig.hasMessageStop = true
		}
	}
	return sig
}

func streamUsageMonotonic(samples []int) bool {
	for i := 1; i < len(samples); i++ {
		if samples[i] < samples[i-1] {
			return false
		}
	}
	return true
}

func streamUsageConsistent(sig anthropicStreamSignals) bool {
	if sig.inputTokens == nil || len(sig.messageDeltaInputSamples) == 0 {
		return true
	}
	for _, s := range sig.messageDeltaInputSamples {
		if s != *sig.inputTokens {
			return false
		}
	}
	return true
}

type streamVerdict struct {
	verdict          string // clean | anomaly | inconclusive
	modelSubstituted bool
	unknownEvents    []string
	findings         []string
}

// analyzeAnthropicStream ports analyze_stream: tri-state verdict over the SSE
// signals. Pure and deterministic.
func analyzeAnthropicStream(sig anthropicStreamSignals) streamVerdict {
	nonPing := 0
	for _, e := range sig.eventTypes {
		if e != "ping" {
			nonPing++
		}
	}
	if len(sig.eventTypes) == 0 || nonPing == 0 {
		return streamVerdict{verdict: "inconclusive",
			findings: []string{"流打开了但没有非 ping 事件——中转要么损坏,要么不说 Anthropic SSE"}}
	}

	unknownSet := map[string]bool{}
	for _, e := range sig.eventTypes {
		if !knownSSEEventTypes[e] {
			unknownSet[e] = true
		}
	}
	unknown := sortedKeys(unknownSet)

	usageMonotonic := streamUsageMonotonic(sig.outputTokensSamples)
	usageConsistent := streamUsageConsistent(sig)
	signatureValid := sig.emptySignatureDeltas == 0
	modelIsClaude := sig.messageStartModel != "" && strings.Contains(strings.ToLower(sig.messageStartModel), "claude")

	var findings []string
	if len(unknown) > 0 {
		capped := unknown
		suffix := ""
		if len(unknown) > maxUnknownEventsReported {
			capped = unknown[:maxUnknownEventsReported]
			suffix = " (+更多,已截断)"
		}
		findings = append(findings, "流里出现未知 SSE 事件类型:"+strings.Join(capped, ", ")+suffix+"——中转在注入/改写事件")
	}
	if !usageMonotonic {
		findings = append(findings, "message_delta 的 output_tokens 出现回退——中转在改写 usage 字段")
	}
	if !usageConsistent {
		findings = append(findings, "message_start 的 input_tokens 与 message_delta 样本不一致——usage 被改写")
	}
	if !signatureValid {
		findings = append(findings, "signature_delta 签名为空——thinking 块降级或被改写")
	}
	if !modelIsClaude {
		if sig.messageStartModel != "" {
			findings = append(findings, "流的 message_start model="+sig.messageStartModel+" 不含 'claude'——中转可能路由到替换模型")
		} else {
			findings = append(findings, "流里没有 message_start.message.model——中转可能剥离模型身份以掩盖降级")
		}
	}

	anomaly := len(unknown) > 0 || !usageMonotonic || !usageConsistent || !signatureValid || !modelIsClaude
	v := "clean"
	if anomaly {
		v = "anomaly"
	}
	cappedUnknown := unknown
	if len(cappedUnknown) > maxUnknownEventsReported {
		cappedUnknown = cappedUnknown[:maxUnknownEventsReported]
	}
	return streamVerdict{verdict: v, modelSubstituted: !modelIsClaude, unknownEvents: cappedUnknown, findings: findings}
}

// anthropicStreamIntegrity issues one streaming probe, inspects the SSE event
// layer, and fails on tampering. A non-Claude stream model is a substitution
// signal (critical); other anomalies are major. Full-mode.
func anthropicStreamIntegrity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	body := anthropicPayload(cfg.Model, "List three primary colors, comma-separated.", 256)
	body["stream"] = true
	res := p.postSSE(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
	if res.err != nil || !res.ok() {
		return detectorSkip("stream-integrity probe could not run")
	}
	sig := collectAnthropicStreamSignals(sseDataObjects(res.text))
	v := analyzeAnthropicStream(sig)

	if v.verdict == "inconclusive" {
		return detectorSkip("stream produced no usable events")
	}
	details := map[string]interface{}{
		"verdict":        v.verdict,
		"event_types":    sig.eventTypes,
		"findings":       v.findings,
		"unknown_events": v.unknownEvents,
	}
	if v.verdict == "anomaly" {
		sev := sevMajor
		code := "stream_tampering"
		if v.modelSubstituted {
			sev = sevCritical
			code = "stream_model_substitution"
		}
		attachIssues(details, []map[string]interface{}{newIssue(sev, code, strings.Join(v.findings, ";"))})
		score := 40.0
		if sev == sevCritical {
			score = 0
		}
		return DetectorResult{Status: "fail", Score: score, Details: details}
	}
	return DetectorResult{Status: "pass", Score: 100, Details: details}
}
