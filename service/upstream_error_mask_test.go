package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const leakyUpstreamMessage = "上游站 example-relay.com 该令牌额度已用尽，请联系站长充值"

func newMaskTestContext(t *testing.T, hideUpstreamErrors bool, role int) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{HideUpstreamErrors: &hideUpstreamErrors})
	if role > 0 {
		c.Set("role", role)
	}
	return c
}

// 脱敏是供应链保密的硬约束：开了开关的渠道，上游原始报错不得出现在任何客户端序列化路径里。
func TestMaskUpstreamErrorForClientMasksUpstreamError(t *testing.T) {
	c := newMaskTestContext(t, true, 0)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: leakyUpstreamMessage,
		Type:    "insufficient_quota",
		Code:    "insufficient_quota",
	}, http.StatusTooManyRequests)

	MaskUpstreamErrorForClient(c, apiErr)

	require.Equal(t, UpstreamErrorMaskedMessage, apiErr.Error())
	openaiOut := apiErr.ToOpenAIError()
	assert.Equal(t, UpstreamErrorMaskedMessage, openaiOut.Message, "OpenAI 序列化路径必须是脱敏文案")
	assert.Equal(t, "insufficient_quota", openaiOut.Type, "非泄漏字段保留")
	claudeOut := apiErr.ToClaudeError()
	assert.NotContains(t, claudeOut.Message, "example-relay.com", "Claude 序列化路径不得泄漏")
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode, "状态码保持原样")
}

func TestMaskUpstreamErrorForClientMasksClaudeError(t *testing.T) {
	c := newMaskTestContext(t, true, 0)
	apiErr := types.WithClaudeError(types.ClaudeError{
		Message: leakyUpstreamMessage,
		Type:    "overloaded_error",
	}, http.StatusInternalServerError)

	MaskUpstreamErrorForClient(c, apiErr)

	assert.Equal(t, UpstreamErrorMaskedMessage, apiErr.ToClaudeError().Message)
	assert.Equal(t, "overloaded_error", apiErr.ToClaudeError().Type)
	assert.NotContains(t, apiErr.ToOpenAIError().Message, "example-relay.com")
}

// 本站自己的错误（余额不足/无可用渠道等）必须保留原文，用户需要这些信息自助。
func TestMaskUpstreamErrorForClientKeepsLocalError(t *testing.T) {
	c := newMaskTestContext(t, true, 0)
	apiErr := types.NewErrorWithStatusCode(errors.New("用户额度不足, 剩余额度: $0.00"),
		types.ErrorCodeInsufficientUserQuota, http.StatusForbidden)

	MaskUpstreamErrorForClient(c, apiErr)

	assert.Contains(t, apiErr.Error(), "用户额度不足")
	assert.Contains(t, apiErr.ToOpenAIError().Message, "用户额度不足")
}

func TestMaskUpstreamErrorRespectsSwitchAndRole(t *testing.T) {
	// 开关关闭：原样
	off := newMaskTestContext(t, false, 0)
	offErr := types.WithOpenAIError(types.OpenAIError{Message: leakyUpstreamMessage}, http.StatusBadGateway)
	MaskUpstreamErrorForClient(off, offErr)
	assert.Contains(t, offErr.Error(), "example-relay.com")

	// 控制台管理员：豁免
	admin := newMaskTestContext(t, true, common.RoleAdminUser)
	adminErr := types.WithOpenAIError(types.OpenAIError{Message: leakyUpstreamMessage}, http.StatusBadGateway)
	MaskUpstreamErrorForClient(admin, adminErr)
	assert.Contains(t, adminErr.Error(), "example-relay.com")

	// 未设置渠道设置（未命中渠道）：不脱敏
	bare, _ := gin.CreateTestContext(httptest.NewRecorder())
	bare.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	assert.False(t, ShouldMaskUpstreamError(bare))
}

// 默认开启：渠道未显式配置 hide_upstream_errors（nil）时，上游错误对非管理员仍脱敏。
func TestMaskUpstreamErrorDefaultsOnWhenUnset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	assert.True(t, ShouldMaskUpstreamError(c), "未配置该开关时默认脱敏")
}
