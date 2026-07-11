package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 护上游契约:上游 200 流里的顶层 error 事件必须转 NewAPIError 走统一错误
// 出口(脱敏/重试),不能原样转发;正常增量(含正文里出现 "error" 字样的)
// 不受影响。
func TestDetectUpstreamStreamError(t *testing.T) {
	t.Run("top-level error event detected", func(t *testing.T) {
		apiErr := detectUpstreamStreamError(`{"error":{"message":"xx站点配额不足, 请访问 https://upstream.example 充值","type":"insufficient_quota"}}`)
		require.NotNil(t, apiErr)
		assert.Contains(t, apiErr.Error(), "配额不足")
	})

	t.Run("error without type but with message detected", func(t *testing.T) {
		apiErr := detectUpstreamStreamError(`{"error":{"message":"upstream exploded"}}`)
		require.NotNil(t, apiErr)
	})

	t.Run("normal delta containing the word error passes", func(t *testing.T) {
		assert.Nil(t, detectUpstreamStreamError(`{"id":"1","choices":[{"delta":{"content":"an \"error\" occurred in your code"}}]}`))
	})

	t.Run("chunk with choices and error field passes to normal flow", func(t *testing.T) {
		assert.Nil(t, detectUpstreamStreamError(`{"choices":[{"delta":{"content":"hi"}}],"error":{"message":"x"}}`))
	})

	t.Run("non json and empty lines pass", func(t *testing.T) {
		assert.Nil(t, detectUpstreamStreamError(""))
		assert.Nil(t, detectUpstreamStreamError("[DONE]"))
		assert.Nil(t, detectUpstreamStreamError(`not json "error"`))
	})

	t.Run("empty error object passes", func(t *testing.T) {
		assert.Nil(t, detectUpstreamStreamError(`{"error":null,"choices":[]}`))
	})
}
