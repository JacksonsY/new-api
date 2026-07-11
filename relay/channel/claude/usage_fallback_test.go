package claude

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newClaudeUsageTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return c
}

// 上游(订阅转 API 类中转)漏报 input_tokens 时，流式结算必须回填请求期预估值，防漏计费。
func TestHandleStreamFinalResponseFallbackWhenInputTokensZero(t *testing.T) {
	c := newClaudeUsageTestContext(t)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{}}
	info.SetEstimatePromptTokens(123)

	claudeInfo := &ClaudeResponseInfo{
		Usage:        &dto.Usage{CompletionTokens: 50},
		ResponseText: strings.Builder{},
		Done:         true,
	}
	HandleStreamFinalResponse(c, info, claudeInfo)

	assert.Equal(t, 123, claudeInfo.Usage.PromptTokens)
	assert.Equal(t, 50, claudeInfo.Usage.CompletionTokens)
	assert.Equal(t, 173, claudeInfo.Usage.TotalTokens)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

func TestHandleStreamFinalResponseFallbackExcludesReportedCacheTokens(t *testing.T) {
	c := newClaudeUsageTestContext(t)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{}}
	info.SetEstimatePromptTokens(200)

	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{
			CompletionTokens: 50,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         60,
				CachedCreationTokens: 40,
			},
		},
		ResponseText: strings.Builder{},
		Done:         true,
	}
	HandleStreamFinalResponse(c, info, claudeInfo)

	assert.Equal(t, 100, claudeInfo.Usage.PromptTokens, "Anthropic input_tokens must exclude cache reads and writes")
	assert.Equal(t, 150, claudeInfo.Usage.TotalTokens)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

// 上游正常报 usage 时不得覆盖上游值。
func TestHandleStreamFinalResponseKeepsUpstreamUsage(t *testing.T) {
	c := newClaudeUsageTestContext(t)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{}}
	info.SetEstimatePromptTokens(123)

	claudeInfo := &ClaudeResponseInfo{
		Usage:        &dto.Usage{PromptTokens: 80, CompletionTokens: 50, TotalTokens: 130},
		ResponseText: strings.Builder{},
		Done:         true,
	}
	HandleStreamFinalResponse(c, info, claudeInfo)

	assert.Equal(t, 80, claudeInfo.Usage.PromptTokens)
	assert.Equal(t, 130, claudeInfo.Usage.TotalTokens)
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

// 非流式：上游 usage 全零时，input 用预估、output 按响应正文本地估算。
func TestHandleClaudeResponseDataFallbackWhenUsageZero(t *testing.T) {
	c := newClaudeUsageTestContext(t)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-3-5-sonnet"},
	}
	info.SetEstimatePromptTokens(200)

	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}, ResponseText: strings.Builder{}}
	body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet",` +
		`"content":[{"type":"text","text":"hello world, this is a fallback billing test"}],` +
		`"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}}`

	handleErr := HandleClaudeResponseData(c, info, claudeInfo, nil, []byte(body))
	require.Nil(t, handleErr)

	wantCompletion := service.EstimateTokenByModel("claude-3-5-sonnet", "hello world, this is a fallback billing test")
	require.Greater(t, wantCompletion, 0)
	assert.Equal(t, 200, claudeInfo.Usage.PromptTokens)
	assert.Equal(t, wantCompletion, claudeInfo.Usage.CompletionTokens)
	assert.Equal(t, 200+wantCompletion, claudeInfo.Usage.TotalTokens)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

func TestHandleClaudeResponseDataFallbackExcludesReportedCacheTokens(t *testing.T) {
	c := newClaudeUsageTestContext(t)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-3-5-sonnet"},
	}
	info.SetEstimatePromptTokens(200)

	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}, ResponseText: strings.Builder{}}
	body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet",` +
		`"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",` +
		`"usage":{"input_tokens":0,"output_tokens":20,"cache_read_input_tokens":60,"cache_creation_input_tokens":40}}`

	handleErr := HandleClaudeResponseData(c, info, claudeInfo, nil, []byte(body))
	require.Nil(t, handleErr)
	assert.Equal(t, 100, claudeInfo.Usage.PromptTokens, "fallback estimate includes cache tokens and must be normalized")
	assert.Equal(t, 120, claudeInfo.Usage.TotalTokens)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

// 非流式：上游正常报 usage 时不得触碰。
func TestHandleClaudeResponseDataKeepsUpstreamUsage(t *testing.T) {
	c := newClaudeUsageTestContext(t)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-3-5-sonnet"},
	}
	info.SetEstimatePromptTokens(200)

	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}, ResponseText: strings.Builder{}}
	body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet",` +
		`"content":[{"type":"text","text":"hi"}],` +
		`"stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":20}}`

	handleErr := HandleClaudeResponseData(c, info, claudeInfo, nil, []byte(body))
	require.Nil(t, handleErr)

	assert.Equal(t, 100, claudeInfo.Usage.PromptTokens)
	assert.Equal(t, 20, claudeInfo.Usage.CompletionTokens)
	assert.Equal(t, 120, claudeInfo.Usage.TotalTokens)
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}
