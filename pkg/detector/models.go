package detector

import "strings"

// modelInfo mirrors Veridrop anthropic/config.py ModelInfo: the per-model
// capability table that gates thinking_signature (applies_to), selects the
// thinking config (extended vs adaptive), the adaptive effort level, and the
// tokenizer-aware input-token delta ranges used by token_usage.
type modelInfo struct {
	alias                    string
	aliases                  []string
	contextTokens            int
	maxOutputTokens          int
	pdfPageMax               int
	supportsExtendedThinking bool
	supportsAdaptiveThinking bool
	newTokenizer             bool
}

// anthropicModels ports config.py MODELS verbatim (DESIGN.md Appendix B).
var anthropicModels = []modelInfo{
	{alias: "claude-opus-4-8", aliases: []string{"claude-opus-4-8"}, contextTokens: 1_000_000, maxOutputTokens: 128_000, pdfPageMax: 600, supportsExtendedThinking: false, supportsAdaptiveThinking: true, newTokenizer: true},
	{alias: "claude-opus-4-7", aliases: []string{"claude-opus-4-7"}, contextTokens: 1_000_000, maxOutputTokens: 128_000, pdfPageMax: 600, supportsExtendedThinking: false, supportsAdaptiveThinking: true, newTokenizer: true},
	{alias: "claude-sonnet-4-6", aliases: []string{"claude-sonnet-4-6"}, contextTokens: 1_000_000, maxOutputTokens: 64_000, pdfPageMax: 600, supportsExtendedThinking: true, supportsAdaptiveThinking: true},
	{alias: "claude-haiku-4-5", aliases: []string{"claude-haiku-4-5"}, contextTokens: 200_000, maxOutputTokens: 64_000, pdfPageMax: 100, supportsExtendedThinking: true, supportsAdaptiveThinking: false},
	{alias: "claude-opus-4-6", aliases: []string{"claude-opus-4-6"}, contextTokens: 1_000_000, maxOutputTokens: 128_000, pdfPageMax: 600, supportsExtendedThinking: true, supportsAdaptiveThinking: false},
	{alias: "claude-sonnet-4-5", aliases: []string{"claude-sonnet-4-5"}, contextTokens: 200_000, maxOutputTokens: 64_000, pdfPageMax: 100, supportsExtendedThinking: true, supportsAdaptiveThinking: false},
	{alias: "claude-opus-4-5", aliases: []string{"claude-opus-4-5"}, contextTokens: 200_000, maxOutputTokens: 64_000, pdfPageMax: 100, supportsExtendedThinking: true, supportsAdaptiveThinking: false},
	{alias: "claude-opus-4-1", aliases: []string{"claude-opus-4-1"}, contextTokens: 200_000, maxOutputTokens: 32_000, pdfPageMax: 100, supportsExtendedThinking: true, supportsAdaptiveThinking: false},
}

// lookupModel matches a user-supplied model ID against known aliases with the
// double-prefix + dot/underscore normalization from config.lookup_model.
// Returns nil when unknown.
func lookupModel(modelID string) *modelInfo {
	nid := normalizeModelID(modelID)
	for i := range anthropicModels {
		for _, alias := range anthropicModels[i].aliases {
			na := normalizeModelID(alias)
			if strings.HasPrefix(nid, na) || strings.HasPrefix(na, nid) {
				return &anthropicModels[i]
			}
		}
	}
	return nil
}

// modelSupportsThinking reports whether thinking_signature applies (config
// applies_to): the model must support extended or adaptive thinking. Unknown
// models return false so the crown-jewel detector skips rather than false-fails.
func modelSupportsThinking(modelID string) bool {
	info := lookupModel(modelID)
	if info == nil {
		return false
	}
	return info.supportsExtendedThinking || info.supportsAdaptiveThinking
}

// adaptiveEffortForModel mirrors thinking_signature._adaptive_effort_for_model:
// opus-4-7/4-8 need xhigh for reliable signed-thinking emission on hard prompts.
func adaptiveEffortForModel(modelID string) string {
	n := normalizeModelID(modelID)
	if strings.HasPrefix(n, "claude-opus-4-7") || strings.HasPrefix(n, "claude-opus-4-8") {
		return "xhigh"
	}
	return "high"
}

// modelUsesNewTokenizer reports whether the model's input-token delta should use
// the wider new-tokenizer range (token_usage._delta_range).
func modelUsesNewTokenizer(modelID string) bool {
	info := lookupModel(modelID)
	return info != nil && info.newTokenizer
}
