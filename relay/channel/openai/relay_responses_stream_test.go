package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newResponsesStreamTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5",
		},
		IsStream: true,
	}
	return c, recorder, resp, info
}

func TestOaiResponsesStreamHandlerReturnsErrorForResponseFailed(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	// 官方规范：顶层 error 事件是扁平结构（code/message/param 在事件顶层，无嵌套 error 对象）。
	body := strings.Join([]string{
		`event: error`,
		`data: {"type":"error","code":"context_length_exceeded","message":"Your input exceeds the context window of this model. Please adjust your input and try again.","param":"input","sequence_number":2}`,
		``,
		`event: response.failed`,
		`data: {"type":"response.failed","response":{"id":"resp_failed","object":"response","created_at":1710000000,"status":"failed","model":"gpt-5.5","error":{"message":"Your input exceeds the context window of this model. Please adjust your input and try again.","code":"context_length_exceeded"}}}`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Contains(t, err.Error(), "context window")
	require.Equal(t, types.ErrorCode("context_length_exceeded"), err.GetErrorCode())
	require.True(t, types.IsSkipRetryError(err))
	require.NotNil(t, info.StreamStatus)
	require.Contains(t, recorder.Body.String(), `event: error`)
	require.Contains(t, recorder.Body.String(), `event: response.failed`)
	require.Contains(t, recorder.Body.String(), `context_length_exceeded`)
}

func TestOaiResponsesStreamHandlerReturnsPendingTopLevelErrorWithoutFailedEvent(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	// 关键回归：官方规范的扁平顶层 error 事件（无嵌套 error 对象），且不发 response.failed
	// 收口。修复前只解析嵌套 error 键、此形状被吞并按成功计费。
	body := strings.Join([]string{
		`event: error`,
		`data: {"type":"error","code":"context_length_exceeded","message":"Your input exceeds the context window of this model. Please adjust your input and try again.","param":"input"}`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Contains(t, err.Error(), "context window")
	require.Equal(t, types.ErrorCode("context_length_exceeded"), err.GetErrorCode())
	require.True(t, types.IsSkipRetryError(err))
	require.Contains(t, recorder.Body.String(), `event: error`)
	require.Contains(t, recorder.Body.String(), `context_length_exceeded`)
}

func TestOaiResponsesStreamHandlerKeepsErrorWhenCompletedCarriesNoOutput(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	// 关键回归：违规兼容上游在真实 error 之后补一个空的 response.completed。
	// 空 completed（无 usage、无图像生成调用）不得作废真实错误、把失败洗成成功
	// 计费。官方规范里 error 是终止事件、其后不会再有 completed。
	body := strings.Join([]string{
		`event: error`,
		`data: {"type":"error","code":"server_error","message":"upstream exploded","param":null}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed"}`,
		``,
	}, "\n")

	c, _, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "upstream exploded")
}

func TestOaiResponsesStreamHandlerExtractsUsageFromCompletedEvent(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp_ok","object":"response","created_at":1710000000,"status":"completed","model":"gpt-5.5","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18,"input_tokens_details":{"cached_tokens":3}}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 11, usage.PromptTokens)
	require.Equal(t, 7, usage.CompletionTokens)
	require.Equal(t, 18, usage.TotalTokens)
	require.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
	require.Contains(t, recorder.Body.String(), `event: response.completed`)
	require.Contains(t, recorder.Body.String(), `"total_tokens":18`)
}
