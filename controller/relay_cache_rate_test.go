package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
)

// 缓存率监控的跨语义归一契约：OpenAI 语义 PromptTokens 已含缓存；
// anthropic 语义为净值需补回缓存读取/创建；未标语义但明显是净值口径
// （总输入小于缓存量）同样补齐。
func TestUsageInputCacheTokens(t *testing.T) {
	t.Run("nil usage", func(t *testing.T) {
		input, cache := usageInputCacheTokens(nil)
		assert.Zero(t, input)
		assert.Zero(t, cache)
	})

	t.Run("openai semantic includes cache in prompt", func(t *testing.T) {
		usage := &dto.Usage{PromptTokens: 1000}
		usage.PromptTokensDetails.CachedTokens = 700
		usage.PromptTokensDetails.CachedCreationTokens = 100

		input, cache := usageInputCacheTokens(usage)
		assert.EqualValues(t, 1000, input)
		assert.EqualValues(t, 700, cache)
	})

	t.Run("anthropic semantic adds cache back", func(t *testing.T) {
		usage := &dto.Usage{PromptTokens: 200, UsageSemantic: "anthropic"}
		usage.PromptTokensDetails.CachedTokens = 700
		usage.PromptTokensDetails.CachedCreationTokens = 100

		input, cache := usageInputCacheTokens(usage)
		assert.EqualValues(t, 1000, input)
		assert.EqualValues(t, 700, cache)
	})

	t.Run("unlabeled net semantics detected by cache exceeding input", func(t *testing.T) {
		usage := &dto.Usage{PromptTokens: 200}
		usage.PromptTokensDetails.CachedTokens = 700

		input, cache := usageInputCacheTokens(usage)
		assert.EqualValues(t, 900, input)
		assert.EqualValues(t, 700, cache)
	})

	t.Run("no cache", func(t *testing.T) {
		usage := &dto.Usage{PromptTokens: 300}
		input, cache := usageInputCacheTokens(usage)
		assert.EqualValues(t, 300, input)
		assert.Zero(t, cache)
	})
}
