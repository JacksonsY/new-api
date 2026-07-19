package xai

import (
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// normalizeXAIUsage 把 xAI 的 usage 归一到本仓库计费口径。
// xAI 的 completion_tokens 不含 reasoning_tokens
// (total = prompt + completion + reasoning)，用 total-prompt 重算把 reasoning
// 计入 completion（计费按 CompletionTokens），再回填 text tokens 明细。
// 流式与非流式两条路径共用此函数，避免口径分叉。
// 保留 total<=prompt 的兜底：异常上游数据下不产生负值 completion。
func normalizeXAIUsage(usage *dto.Usage) {
	if usage == nil {
		return
	}
	if usage.TotalTokens > usage.PromptTokens {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}
	// grok reasoning 模型偶发返回 reasoning_tokens 超过有效输出的异常 usage
	// （见 grok-4-fast-reasoning 负 text_output_tokens 报告），钳 0 避免 text 明细
	// 变负：对齐项目「TextTokens 非负」的既有不变量（calculateAudioQuota 亦 max(_,0)），
	// 并防止日志详情 text_output 展示负数。计费按 CompletionTokens（非负）不受影响。
	textTokens := usage.CompletionTokens - usage.CompletionTokenDetails.ReasoningTokens
	if textTokens < 0 {
		textTokens = 0
	}
	usage.CompletionTokenDetails.TextTokens = textTokens
}

func streamResponseXAI2OpenAI(xAIResp *dto.ChatCompletionsStreamResponse, usage *dto.Usage) *dto.ChatCompletionsStreamResponse {
	if xAIResp == nil {
		return nil
	}
	if xAIResp.Usage != nil {
		xAIResp.Usage.CompletionTokens = usage.CompletionTokens
	}
	openAIResp := &dto.ChatCompletionsStreamResponse{
		Id:      xAIResp.Id,
		Object:  xAIResp.Object,
		Created: xAIResp.Created,
		Model:   xAIResp.Model,
		Choices: xAIResp.Choices,
		Usage:   xAIResp.Usage,
	}

	return openAIResp
}

func xAIStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	usage := &dto.Usage{}
	var responseTextBuilder strings.Builder
	var toolCount int
	var containStreamUsage bool

	helper.SetEventStreamHeaders(c)

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var xAIResp *dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &xAIResp); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			sr.Error(err)
			return
		}

		// 把 xAI 的usage转换为 OpenAI 的usage
		if xAIResp.Usage != nil {
			containStreamUsage = true
			// 整 struct 拷贝（与 openai.handleLastResponse 同款）保留
			// prompt_tokens_details.cached_tokens 等明细；只重建三个标量
			// 会把流式请求的缓存命中 token 全部丢掉，导致按全价计费。
			*usage = *xAIResp.Usage
			normalizeXAIUsage(usage)
		}

		openaiResponse := streamResponseXAI2OpenAI(xAIResp, usage)
		_ = openai.ProcessStreamResponse(*openaiResponse, &responseTextBuilder, &toolCount)
		if err := helper.ObjectData(c, openaiResponse); err != nil {
			common.SysLog(err.Error())
			sr.Error(err)
		}
	})

	if !containStreamUsage {
		usage = service.ResponseText2Usage(c, responseTextBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		usage.CompletionTokens += toolCount * 7
	}

	helper.Done(c)
	service.CloseResponseBodyGracefully(resp)
	return usage, nil
}

func xAIHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	var xaiResponse ChatCompletionResponse
	err = common.Unmarshal(responseBody, &xaiResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	normalizeXAIUsage(xaiResponse.Usage)

	// new body
	encodeJson, err := common.Marshal(xaiResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	service.IOCopyBytesGracefully(c, resp, encodeJson)

	return xaiResponse.Usage, nil
}
