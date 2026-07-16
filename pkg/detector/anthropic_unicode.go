package detector

// anthropic_unicode.go ports the Unicode-fidelity probe from LLMprobe-engine
// (@bazaarlink/probe-engine, src/probe-suite.ts symbol_exact, AGPL-3.0). It asks
// the model to output CJK corner-bracket text verbatim and checks it survives
// intact — a relay that NFC/NFKC-normalizes, substitutes brackets, or re-encodes
// text (a transcoding middleman) alters it. Independent Go reimplementation,
// attribution retained per the AGPL, additive, does not replace any new-api /
// QuantumNous identity.

import (
	"context"
	"strings"
)

// The corner brackets 「」 (U+300C/U+300D) are the transcoding canary: NFKC
// normalization folds them, so their survival distinguishes a passthrough relay
// from a re-encoding one. 這是測試 is the traditional-Chinese core payload.
const (
	unicodeExpectFull = "「這是測試」"
	unicodeCoreCJK    = "這是測試"
)

// anthropicUnicodeFidelity checks verbatim Unicode passthrough. Exact match →
// pass; core CJK present but corner brackets folded/substituted → minor (a
// transcoding relay); core CJK absent → inconclusive skip (model may not have
// complied), never a false fail. Full-mode.
func anthropicUnicodeFidelity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	prompt := "Output exactly this text and nothing else, character for character: " + unicodeExpectFull
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
		anthropicPayload(cfg.Model, prompt, 64))
	if res.err != nil || !res.ok() {
		return detectorSkip("unicode-fidelity probe could not run")
	}
	text := anthropicText(res.parsed)

	if strings.Contains(text, unicodeExpectFull) {
		return DetectorResult{Status: "pass", Score: 100, Details: map[string]interface{}{"exact": true}}
	}
	if !strings.Contains(text, unicodeCoreCJK) {
		return detectorSkip("model did not reproduce the CJK payload")
	}
	// Core survived but the corner brackets did not — a normalizing/re-encoding
	// middleman folded them.
	details := map[string]interface{}{"exact": false, "core_present": true}
	attachIssues(details, []map[string]interface{}{newIssue(sevMinor, "unicode_transcoded",
		"CJK 正文透传但角括号「」被折叠/替换——疑似做 NFC/NFKC 归一或重编码的转码中转")})
	return DetectorResult{Status: "fail", Score: 60, Details: details}
}
