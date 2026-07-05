package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// UpstreamErrorMaskedMessage 脱敏后回给下游客户的通用文案（状态码保持原样）。
const UpstreamErrorMaskedMessage = "上游服务暂时不可用，请稍后重试"

// ShouldMaskUpstreamError 当前请求命中的渠道开启了错误脱敏、且请求者非控制台管理员时为 true。
// 角色豁免只对带 session 角色的路径（控制台/Playground）生效；API Key 请求一律脱敏——
// key 可能外流，对管理员自己的 key 也脱敏更安全，排障走服务端日志与错误日志 admin_info。
func ShouldMaskUpstreamError(c *gin.Context) bool {
	if c == nil {
		return false
	}
	setting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	if !ok || !setting.HideUpstreamErrors {
		return false
	}
	return c.GetInt("role") < common.RoleAdminUser
}

// MaskUpstreamErrorForClient 对即将写回客户端的错误应用脱敏：
// 仅上游来源的错误（openai/claude/gemini 等 errorType）换通用文案，
// 本站生成的 new_api_error（余额不足、无可用渠道等）保留原文。
// 必须在原始报错记入服务端日志之后调用。
func MaskUpstreamErrorForClient(c *gin.Context, apiErr *types.NewAPIError) {
	if apiErr == nil || apiErr.GetErrorType() == types.ErrorTypeNewAPIError {
		return
	}
	if !ShouldMaskUpstreamError(c) {
		return
	}
	apiErr.MaskClientMessage(UpstreamErrorMaskedMessage)
}
