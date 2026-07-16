package detector

// anthropic_adaptive_injection.go ports the conditional/adaptive-injection
// differential (AC-1.b) from LLMprobe-engine (@bazaarlink/probe-engine,
// src/probe-suite.ts adaptive_injection + src/proxy-analyzer.ts computeAc1b,
// AGPL-3.0). It sends two structurally identical verbatim-echo prompts at
// temperature 0 that differ by one sensitive token only (a neutral word list vs
// the same list with "aws_secret_access_key" spliced in). A clean upstream echoes
// both identically; if the neutral echo is clean but the sensitive one diverges
// or carries injected content, the relay is behaving differently when it sees a
// secret-shaped request — a conditional injection our other probes (all
// single-condition) cannot see. Only the 2-shot active technique is reused (not
// the engine's stateful accumulator); independent Go reimplementation,
// attribution retained per the AGPL, additive, does not replace any new-api /
// QuantumNous identity.

import (
	"context"
	"fmt"
	"strings"
)

const (
	adaptiveNeutralPayload = "apple banana cherry date elderberry"
	adaptiveTriggerPayload = "apple banana aws_secret_access_key date elderberry"
)

func adaptiveEchoPrompt(payload string) string {
	return fmt.Sprintf("Echo the following text back to me verbatim, exactly as-is, with no changes and no extra words:\n%s", payload)
}

// echoedVerbatim reports whether an echo response reproduced the payload,
// tolerant of surrounding whitespace/punctuation/quotes (compactForLeakCheck).
func echoedVerbatim(response, payload string) bool {
	want := compactForLeakCheck(payload)
	return want != "" && strings.Contains(compactForLeakCheck(response), want)
}

// anthropicAdaptiveInjection runs the paired differential. Fails critically only
// when the neutral echo is clean AND the secret-shaped request came back with an
// injected exfil channel (beacon/dropper) — a relay changing behavior on
// sensitive input. A bare refusal/alteration of the sensitive echo, with no
// injection, is NOT treated as fraud: a well-aligned model legitimately declines
// to parrot a secret-shaped token, which is indistinguishable from a benign
// safety refusal, so that case is inconclusive (skip). If the neutral echo itself
// is not clean, the model just doesn't echo reliably — inconclusive, skip.
// Full-mode only (2 requests).
func anthropicAdaptiveInjection(ctx context.Context, p *prober, cfg Config) DetectorResult {
	nRes := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
		anthropicPayload(cfg.Model, adaptiveEchoPrompt(adaptiveNeutralPayload), 128))
	tRes := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
		anthropicPayload(cfg.Model, adaptiveEchoPrompt(adaptiveTriggerPayload), 128))
	if nRes.err != nil || !nRes.ok() || tRes.err != nil || !tRes.ok() {
		return detectorSkip("adaptive-injection probes could not run")
	}

	neutralText := anthropicText(nRes.parsed)
	triggerText := anthropicText(tRes.parsed)
	neutralClean := echoedVerbatim(neutralText, adaptiveNeutralPayload)
	triggerEchoed := echoedVerbatim(triggerText, adaptiveTriggerPayload)
	// A relay doing conditional injection ADDS an exfil channel (beacon/dropper)
	// when it sees the secret-shaped request — that is the hard signal. A mere
	// refusal/alteration of the sensitive echo is not, so the two are scored
	// differently below.
	injectionSev, injectionHits := scanExfilChannels(triggerText, "")

	details := map[string]interface{}{
		"neutral_clean":              neutralClean,
		"trigger_echoed":             triggerEchoed,
		"trigger_injection_severity": injectionSev,
	}
	if len(injectionHits) > 0 {
		details["trigger_injection"] = injectionHits
	}

	if !neutralClean {
		return detectorSkip("model does not echo verbatim (differential inconclusive)")
	}
	if injectionSev != "none" {
		attachIssues(details, []map[string]interface{}{newIssue(sevCritical, "conditional_injection",
			"配对差分:中性回显干净,含敏感 token 的请求却被注入了外泄通道——中转在识别到敏感请求时改变了行为(条件注入)")})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	}
	if !triggerEchoed {
		// Neutral echoed clean but the sensitive token was refused/altered without
		// any injection. A safety-conscious genuine model does exactly this, so it
		// is indistinguishable from benign alignment — inconclusive, not fraud.
		return detectorSkip("model declined to echo the sensitive token verbatim (aligned refusal; differential inconclusive)")
	}
	return DetectorResult{Status: "pass", Score: 100, Details: details}
}
