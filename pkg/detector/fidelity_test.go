package detector

// 保真度回归：锁定对齐真 Veridrop 的关键修复（usage 指纹分级、品牌表、
// modelMatches 归一、模式成员）。均为纯函数，无网络。

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsageFingerprintClassification(t *testing.T) {
	// claude_* → critical (claude category)
	fp := scanOpenAIUsageFingerprints(map[string]interface{}{"claude_cache_creation_5m": 1})
	assert.NotEmpty(t, fp.claude)
	assert.True(t, fp.hasCritical())
	assert.Empty(t, fp.minor)

	// gemini 令牌计数键 → critical (gemini category)
	fp = scanOpenAIUsageFingerprints(map[string]interface{}{"candidates_token_count": 3})
	assert.NotEmpty(t, fp.gemini)
	assert.True(t, fp.hasCritical())

	// usage_source 非 openai（任意后端、大小写不敏感）→ critical (source)
	fp = scanOpenAIUsageFingerprints(map[string]interface{}{"usage_source": "Anthropic"})
	assert.Equal(t, "anthropic", fp.source)
	fp = scanOpenAIUsageFingerprints(map[string]interface{}{"usage_source": "vertex"})
	assert.Equal(t, "vertex", fp.source)

	// 多类别共存 → 各自独立 critical（不再合并成一条）
	fp = scanOpenAIUsageFingerprints(map[string]interface{}{
		"candidates_token_count": 3, "usage_source": "google",
	})
	assert.NotEmpty(t, fp.gemini)
	assert.Equal(t, "google", fp.source)

	// usage_source == openai → 干净
	fp = scanOpenAIUsageFingerprints(map[string]interface{}{"usage_source": "openai"})
	assert.False(t, fp.hasCritical())
	assert.Empty(t, fp.minor)

	// input/output token 字段 → MINOR（不再误判 critical）
	fp = scanOpenAIUsageFingerprints(map[string]interface{}{"input_tokens": 5, "output_tokens": 3})
	assert.False(t, fp.hasCritical())
	assert.NotEmpty(t, fp.minor)

	// cache_creation_input_tokens → 完全不 flag（对齐真源码）
	fp = scanOpenAIUsageFingerprints(map[string]interface{}{"cache_creation_input_tokens": 9})
	assert.False(t, fp.hasCritical())
	assert.Empty(t, fp.minor)
}

func TestBrandTableCoverage(t *testing.T) {
	cases := []struct{ text, brand string }{
		{"I am LLaMA by Meta", "LLaMA"},
		{"powered by Mistral", "Mistral"},
		{"this is Bard", "Bard"},
		{"我是文心一言", "Wenxin"},
		{"running on Copilot", "Copilot"},
		{"Doubao here", "Doubao"},
		{"backed by AWS Bedrock", "AWS Bedrock"},
		{"it's Kiro", "Kiro"},
	}
	for _, tc := range cases {
		assert.Contains(t, scanBrands(tc.text), tc.brand, tc.text)
	}
	// 真 Claude 自述不误报
	assert.Empty(t, scanBrands("I'm Claude, made by Anthropic."))
}

func TestModelMatchesNormalization(t *testing.T) {
	assert.True(t, modelMatches("claude-sonnet-4.5", "claude-sonnet-4-5-20250101"))
	assert.True(t, modelMatches("models/gemini-2.5-flash", "gemini-2.5-flash"))
	assert.True(t, modelMatches("gpt-5_4-mini", "gpt-5-4-mini"))
	assert.False(t, modelMatches("claude-opus", "gpt-4o"))
}

func TestAnthropicQuickModeMembership(t *testing.T) {
	names := map[string]bool{}
	for _, d := range selectDetectors(Config{Protocol: "anthropic", Mode: "quick"}) {
		names[d.name] = true
	}
	assert.True(t, names[detectorThinkingSignature], "quick 必须含 thinking_signature（加密锚点）")
	assert.False(t, names[detectorTokenUsage], "quick 不应含 token_usage")
	assert.False(t, names[detectorBehavioral], "behavioral 为 full-only")
}

func TestOpenAIModeMembership(t *testing.T) {
	quick := map[string]bool{}
	for _, d := range selectDetectors(Config{Protocol: "openai", Mode: "quick"}) {
		quick[d.name] = true
	}
	assert.False(t, quick[detectorFunctionCalling], "openai quick 不应含 function_calling")

	std := map[string]bool{}
	for _, d := range selectDetectors(Config{Protocol: "openai", Mode: "standard"}) {
		std[d.name] = true
	}
	assert.True(t, std[detectorFunctionCalling], "standard 应含 function_calling")
	assert.False(t, std[detectorTokenParity], "token_parity 真源码不注册，不应出现在任何模式")
}
