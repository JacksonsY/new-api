package detector

// chat_identity.go gives the OpenAI-compatible chat surface (openai / grok /
// gemini protocols) an identity detector — the chat analogue of
// anthropicIdentity. It asks the model who built it and scores the answer
// against the vendor the target model name promises: a "grok-4.5" endpoint
// answering "I am ChatGPT by OpenAI" is core-swap evidence, and vice versa.
//
// Calibration deviates from the Anthropic 4-tier on two points, deliberately:
// ANY own-brand marker counts as a full identity hit (chat-surface models often
// name only the product or only the company), and a brand-free answer scores
// 60/pass instead of 0/fail (relays routinely inject system prompts that mute
// self-identification; absence of evidence is not evidence of fraud). Foreign
// brands stay the hard signal either way. Additive; does not replace any
// new-api / QuantumNous identity.

import (
	"context"
	"regexp"
	"strings"
)

// chatVendorFor maps a target model name to the vendor its name promises.
// Unknown vendors (llama / deepseek / qwen …) return "" — identity is then
// recorded as evidence only, never scored against a brand table we can't anchor.
func chatVendorFor(model string) string {
	n := normalizeModelID(model)
	switch {
	case strings.HasPrefix(n, "claude"):
		return "Anthropic"
	case strings.HasPrefix(n, "grok"):
		return "xAI"
	case strings.HasPrefix(n, "gemini"):
		return "Google"
	case strings.HasPrefix(n, "gpt") || strings.HasPrefix(n, "chatgpt") || reOSeries.MatchString(n):
		return "OpenAI"
	}
	return ""
}

var reOSeries = regexp.MustCompile(`^o[0-9]`)

// Per-vendor own-brand markers. Any single hit is a full identity confirmation
// (see calibration note above).
var chatVendorOwnPatterns = map[string][]brandPattern{
	"OpenAI": {
		{regexp.MustCompile(`(?i)\bchatgpt\b`), "ChatGPT"},
		{regexp.MustCompile(`(?i)\bopenai\b`), "OpenAI"},
		{regexp.MustCompile(`(?i)\bgpt[-\s]?[345](\.\d+)?\b`), "GPT-3/4/5"},
	},
	"xAI": {
		{regexp.MustCompile(`(?i)\bgrok\b`), "Grok"},
		{regexp.MustCompile(`(?i)\bxai\b|\bx\.ai\b`), "xAI"},
	},
	"Google": {
		{regexp.MustCompile(`(?i)\bgemini\b`), "Gemini"},
		{regexp.MustCompile(`(?i)\bgoogle\b`), "Google"},
		{regexp.MustCompile(`(?i)\bbard\b`), "Bard"},
	},
	"Anthropic": {
		{regexp.MustCompile(`(?i)\bclaude\b`), "Claude"},
		{regexp.MustCompile(`(?i)\banthropic\b`), "Anthropic"},
	},
}

// chatVendorOwnLabels names the brand-table labels that belong to each vendor's
// own family, so a genuine self-id is never counted as a foreign brand.
var chatVendorOwnLabels = map[string]map[string]bool{
	"OpenAI":    {"ChatGPT": true, "OpenAI": true, "GPT-3/4/5": true},
	"xAI":       {"Grok": true, "xAI": true},
	"Google":    {"Gemini": true, "Bard": true},
	"Anthropic": {"Claude": true, "Anthropic": true},
}

// anthropicFamilyPatterns extends the shared foreign-brand table for the chat
// surface: brandPatterns deliberately excludes Claude/Anthropic (they are the
// anthropic protocol's REQUIRED markers), but on a gpt/grok/gemini target they
// are foreign evidence like any other.
var anthropicFamilyPatterns = []brandPattern{
	{regexp.MustCompile(`(?i)\bclaude\b`), "Claude"},
	{regexp.MustCompile(`(?i)\banthropic\b`), "Anthropic"},
}

// scanChatForeignBrands returns the distinct brand labels found in text that do
// NOT belong to the expected vendor's own family, in table order.
func scanChatForeignBrands(text, vendor string) []string {
	own := chatVendorOwnLabels[vendor]
	var out []string
	seen := make(map[string]bool)
	scan := func(patterns []brandPattern) {
		for _, bp := range patterns {
			if own[bp.label] || seen[bp.label] || !bp.re.MatchString(text) {
				continue
			}
			seen[bp.label] = true
			out = append(out, bp.label)
		}
	}
	scan(brandPatterns)
	scan(anthropicFamilyPatterns)
	return out
}

// chatIdentity asks the model who it is and scores the self-report against the
// vendor its model name promises. Registered as the `identity` detector for the
// openai / grok / gemini protocols; detail keys feed applyIdentityFindings so
// the report header surfaces the self-id and any foreign brands.
func chatIdentity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	// Same probe question as the anthropic surface, routed to the protocol's
	// native chat surface (gemini → generateContent). 300 tokens leaves room for
	// hidden reasoning (grok-4.x / gpt-5.x burn budget before the visible sentence).
	res := p.probeChat(ctx, cfg.Model, anthropicIdentityPrompt, 300)
	details := map[string]interface{}{"status_code": res.statusCode}
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error(), Details: details}
	}
	if !res.ok() {
		msg := upstreamErrorText(res)
		details["error"] = msg
		return DetectorResult{Status: "error", Score: 0, Error: msg, Details: details}
	}
	text := p.chatContent(res)
	if strings.TrimSpace(text) == "" {
		// Reasoning budget exhausted / empty content: no evidence either way.
		return detectorSkip("identity probe returned empty content (reasoning budget likely exhausted)")
	}

	vendor := chatVendorFor(cfg.Model)
	details["self_reported_identity"] = text
	details["response_text"] = truncate(text, 300)
	details["expected_vendor"] = vendor

	if vendor == "" {
		// Open-weights / unknown vendor: no anchored brand table, record only.
		details["summary"] = "模型厂商未知,自述身份仅作记录: " + truncate(text, 120)
		return DetectorResult{Status: "pass", Score: 100, Details: details}
	}

	ownHits := make([]string, 0, 2)
	for _, bp := range chatVendorOwnPatterns[vendor] {
		if bp.re.MatchString(text) {
			ownHits = append(ownHits, bp.label)
		}
	}
	foreign := scanChatForeignBrands(text, vendor)
	details["required_hits"] = ownHits
	details["detected_foreign_brands"] = foreign

	switch {
	case len(ownHits) > 0 && len(foreign) == 0:
		details["summary"] = "自述身份与 " + vendor + " 一致: " + strings.Join(ownHits, ", ")
		return DetectorResult{Status: "pass", Score: 100, Details: details}
	case len(ownHits) > 0:
		attachIssues(details, []map[string]interface{}{newIssue(sevMajor, "identity_mixed_brands",
			"自述含 "+vendor+" 标识但混入异源品牌("+strings.Join(foreign, ", ")+")——疑似混拼/泄漏后端")})
		return DetectorResult{Status: "fail", Score: 30, Details: details}
	case len(foreign) > 0:
		attachIssues(details, []map[string]interface{}{newIssue(sevMajor, "identity_foreign_brand",
			"目标模型属 "+vendor+",自述却是异源品牌("+strings.Join(foreign, ", ")+")——疑似换核")})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	default:
		details["summary"] = "自述未含品牌标识(中转注入系统提示时常见,不作伪证)"
		return DetectorResult{Status: "pass", Score: 60, Details: details}
	}
}
