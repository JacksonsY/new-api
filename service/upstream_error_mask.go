package service

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// 脱敏后回给下游客户的通用文案(状态码保持原样)。中英双语:relay 错误直达
// API 客户端,不经会话语言,双语让中/外用户都能理解。
const (
	// UpstreamErrorMaskedMessage 5xx/429/网络类:重试可能恢复。
	UpstreamErrorMaskedMessage = "上游服务暂时不可用，请稍后重试 (upstream temporarily unavailable, please retry later)"
	// UpstreamErrorMaskedMessageClient 4xx(非 429):请求本身被上游拒绝,重试
	// 不会成功(如 context length exceeded / image too large),用"请检查请求"
	// 而非"稍后重试",避免误导客户端无谓重试。
	UpstreamErrorMaskedMessageClient = "上游拒绝了本次请求，请检查请求内容 (upstream rejected the request, please check your request)"
)

// MaskedMessageForStatus 按上游状态码选脱敏文案:4xx(除 429 限流)是客户端
// 可自查的错误,不能一律换成"稍后重试"。
func MaskedMessageForStatus(statusCode int) string {
	if statusCode >= 400 && statusCode < 500 && statusCode != http.StatusTooManyRequests {
		return UpstreamErrorMaskedMessageClient
	}
	return UpstreamErrorMaskedMessage
}

// ShouldMaskUpstreamError 默认对命中渠道开启错误脱敏（仅渠道显式关闭时才透传上游原文），
// 且请求者非控制台管理员时为 true。角色豁免只对带 session 角色的路径（控制台/Playground）
// 生效；API Key 请求一律脱敏——key 可能外流，对管理员自己的 key 也脱敏更安全，
// 排障走服务端日志与错误日志 admin_info。
func ShouldMaskUpstreamError(c *gin.Context) bool {
	if c == nil {
		return false
	}
	setting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	if !ok {
		return false
	}
	// 默认开启：仅当渠道显式关闭（hide_upstream_errors=false）时才透传上游原文
	if setting.HideUpstreamErrors != nil && !*setting.HideUpstreamErrors {
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
	apiErr.MaskClientMessage(MaskedMessageForStatus(apiErr.StatusCode))
}
