package xai

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
)

// xAI 的 completion_tokens 不含 reasoning_tokens；计费按 CompletionTokens，
// 归一必须把 reasoning 计入，否则 grok reasoning 模型漏计费。
func TestNormalizeXAIUsageFoldsReasoningIntoCompletion(t *testing.T) {
	cases := []struct {
		name           string
		usage          dto.Usage
		wantCompletion int
		wantText       int
	}{
		{
			name: "reasoning model: completion excludes reasoning",
			usage: dto.Usage{
				PromptTokens:     100,
				CompletionTokens: 5, // 上游只给可见输出，不含 reasoning
				TotalTokens:      115,
				CompletionTokenDetails: dto.OutputTokenDetails{
					ReasoningTokens: 10,
				},
			},
			wantCompletion: 15, // total-prompt = 5 可见 + 10 reasoning
			wantText:       5,  // completion - reasoning
		},
		{
			name: "pure cache hit: total==prompt keeps zero completion",
			usage: dto.Usage{
				PromptTokens:     200,
				CompletionTokens: 0,
				TotalTokens:      200,
			},
			wantCompletion: 0,
			wantText:       0,
		},
		{
			name: "no reasoning: completion equals total-prompt",
			usage: dto.Usage{
				PromptTokens:     50,
				CompletionTokens: 30,
				TotalTokens:      80,
			},
			wantCompletion: 30,
			wantText:       30,
		},
		{
			name: "malformed total<prompt: keep upstream, no negative",
			usage: dto.Usage{
				PromptTokens:     100,
				CompletionTokens: 7,
				TotalTokens:      90,
			},
			wantCompletion: 7,
			wantText:       7,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usage := tc.usage
			normalizeXAIUsage(&usage)
			assert.Equal(t, tc.wantCompletion, usage.CompletionTokens)
			assert.Equal(t, tc.wantText, usage.CompletionTokenDetails.TextTokens)
		})
	}
}

func TestNormalizeXAIUsageNilSafe(t *testing.T) {
	assert.NotPanics(t, func() { normalizeXAIUsage(nil) })
}

// 归一必须保留 struct 拷贝带来的缓存命中明细（PR #6145 的修复不能被回退）。
func TestNormalizeXAIUsagePreservesCachedTokens(t *testing.T) {
	usage := dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		TotalTokens:      120,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 80,
		},
	}
	normalizeXAIUsage(&usage)
	assert.Equal(t, 80, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 20, usage.CompletionTokens)
}
