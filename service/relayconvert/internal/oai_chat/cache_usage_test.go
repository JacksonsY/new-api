package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/require"
)

// /v1/messages 响应契约：Anthropic 语义的 input_tokens 不含缓存读取/创建，
// 三者并列。OpenAI 语义的 prompt_tokens 是包含缓存的总输入，转换时必须扣除，
// 否则客户端与下游计费方会把缓存部分重复计算。
func TestBuildClaudeUsageFromOpenAIUsageExcludesCacheFromInput(t *testing.T) {
	usage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 20,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         800,
			CachedCreationTokens: 100,
		},
	})

	require.NotNil(t, usage)
	require.Equal(t, 100, usage.InputTokens)
	require.Equal(t, 800, usage.CacheReadInputTokens)
	require.Equal(t, 100, usage.CacheCreationInputTokens)
	require.Equal(t, 20, usage.OutputTokens)
	// input + cache_read + cache_creation 应还原为原始总输入
	require.Equal(t, 1000, usage.InputTokens+usage.CacheReadInputTokens+usage.CacheCreationInputTokens)
}

func TestBuildClaudeUsageFromOpenAIUsageKeepsAnthropicSemanticInput(t *testing.T) {
	usage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		PromptTokens:     200,
		CompletionTokens: 20,
		UsageSemantic:    dto.BillingUsageSemanticAnthropic,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 800,
		},
	})

	require.NotNil(t, usage)
	require.Equal(t, 200, usage.InputTokens)
	require.Equal(t, 800, usage.CacheReadInputTokens)
}

func TestBuildClaudeUsageFromOpenAIUsageClampsNegativeInput(t *testing.T) {
	usage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		PromptTokens: 500,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 800,
		},
	})

	require.NotNil(t, usage)
	require.Equal(t, 0, usage.InputTokens)
}

func TestBuildClaudeUsageFromOpenAIUsageKeepsCacheCreationSplit(t *testing.T) {
	usage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		PromptTokens: 1000,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 100,
		},
		ClaudeCacheCreation5mTokens: 30,
		ClaudeCacheCreation1hTokens: 20,
	})

	require.NotNil(t, usage)
	require.Equal(t, 900, usage.InputTokens)
	require.NotNil(t, usage.CacheCreation)
	// 聚合值多出的部分归入 5m 档（NormalizeCacheCreationSplit 现有行为）
	require.Equal(t, 80, usage.CacheCreation.Ephemeral5mInputTokens)
	require.Equal(t, 20, usage.CacheCreation.Ephemeral1hInputTokens)
}
