package detector

// anthropic_hidden_prompt.go ports the prompt-token-floor test from
// LLMprobe-engine (@bazaarlink/probe-engine, src/token-inflation.ts +
// src/probe-suite.ts token_inflation, AGPL-3.0). A minimal 1-token request
// ("Hi") whose reported input_tokens far exceeds the floor means a wrapper
// injected a hidden system prompt (the engine fingerprints Kiro / Amazon-Q
// coding agents that prepend a ~2000-token prompt). This refines token_usage
// (self-consistency of reported vs counted tokens) with a distinct minimal-input
// floor test. Independent Go reimplementation, attribution retained per the
// AGPL, additive, does not replace any new-api / QuantumNous identity.

import "context"

// hiddenPromptTokenFloor is the input_tokens ceiling for a minimal "Hi" request
// with no system prompt. Genuine Anthropic reports ~8-12; anything far above
// means the relay prepended a hidden prompt (LLMprobe TOKEN_INFLATION_THRESHOLD).
const hiddenPromptTokenFloor = 50

// anthropicHiddenPromptFloor sends a minimal prompt and fails if the reported
// input token count is inflated by a hidden injected system prompt. Full-mode.
func anthropicHiddenPromptFloor(ctx context.Context, p *prober, cfg Config) DetectorResult {
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
		anthropicPayload(cfg.Model, "Hi", 16))
	if res.err != nil || !res.ok() {
		return detectorSkip("hidden-prompt floor probe could not run")
	}
	usage := subMap(res.parsed, "usage")
	inTok, ok := intField(usage, "input_tokens")
	if !ok {
		return detectorSkip("no input_tokens reported")
	}

	details := map[string]interface{}{"input_tokens": inTok, "floor": hiddenPromptTokenFloor}
	if inTok > hiddenPromptTokenFloor {
		attachIssues(details, []map[string]interface{}{newIssue(sevMajor, "hidden_prompt_injected",
			"极小请求(\"Hi\",无 system)却报了远超地板的 input_tokens——中转/包装层注入了隐藏系统提示")})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	}
	return DetectorResult{Status: "pass", Score: 100, Details: details}
}
