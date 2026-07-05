package common

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 响应体模型名脱敏：模型映射生效时，把上游返回的真实模型名改写回客户请求的原始名，
// 防止下游从响应 model 字段反推背后的真实上游型号/供应商。
// 生效条件由 ContextKeyClientFacingModelName 控制（relay/helper.ModelMappedHelper 按次设置），
// 未映射的请求零成本直通。

// responseModelKeys 覆盖各协议响应里携带模型名的 JSON 路径：
// openai/embeddings/rerank 顶层 model、claude 流式 message_start 的 message.model、
// Responses API 流式事件的 response.model、gemini 的 modelVersion。
var responseModelKeys = []string{"model", "message.model", "response.model", "modelVersion"}

// MaskResponseModelNameString 对单个 SSE data 行 / JSON 字符串做模型名改写。
func MaskResponseModelNameString(c *gin.Context, data string) string {
	origin := clientFacingModelName(c)
	if origin == "" || len(data) == 0 || data[0] != '{' {
		return data
	}
	for _, key := range responseModelKeys {
		value := gjson.Get(data, key)
		if !value.Exists() || value.Type != gjson.String || value.String() == origin {
			continue
		}
		if patched, err := sjson.Set(data, key, origin); err == nil {
			data = patched
		}
	}
	return data
}

// MaskResponseModelName 对完整响应体做模型名改写（非流式路径）。
func MaskResponseModelName(c *gin.Context, data []byte) []byte {
	origin := clientFacingModelName(c)
	if origin == "" || len(data) == 0 || data[0] != '{' {
		return data
	}
	for _, key := range responseModelKeys {
		value := gjson.GetBytes(data, key)
		if !value.Exists() || value.Type != gjson.String || value.String() == origin {
			continue
		}
		if patched, err := sjson.SetBytes(data, key, origin); err == nil {
			data = patched
		}
	}
	return data
}

func clientFacingModelName(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(GetContextKeyString(c, constant.ContextKeyClientFacingModelName))
}
