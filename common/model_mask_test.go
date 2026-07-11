package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newModelMaskContext(t *testing.T, origin string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if origin != "" {
		SetContextKey(c, constant.ContextKeyClientFacingModelName, origin)
	}
	return c
}

// 模型映射生效时，四类协议路径的响应模型名都必须被改写回客户请求名——漏一条就是供应链泄漏。
func TestMaskResponseModelNameRewritesAllProtocolPaths(t *testing.T) {
	c := newModelMaskContext(t, "gpt-4o")
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "openai 非流式/chunk 顶层 model",
			in:   `{"id":"chatcmpl-1","model":"gpt-4o-mini-upstream","choices":[]}`,
			want: `"model":"gpt-4o"`,
		},
		{
			name: "claude message_start 嵌套 message.model",
			in:   `{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-5-internal","usage":{"input_tokens":10}}}`,
			want: `"model":"gpt-4o"`,
		},
		{
			name: "responses api 流式事件 response.model",
			in:   `{"type":"response.created","response":{"id":"resp_1","model":"gpt-4o-mini-upstream"}}`,
			want: `"model":"gpt-4o"`,
		},
		{
			name: "gemini modelVersion",
			in:   `{"candidates":[],"modelVersion":"gemini-2.5-pro-internal"}`,
			want: `"modelVersion":"gpt-4o"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := MaskResponseModelNameString(c, tt.in)
			assert.Contains(t, out, tt.want)
			assert.NotContains(t, out, "upstream")
			assert.NotContains(t, out, "internal")
			// bytes 版本行为一致
			assert.Equal(t, out, string(MaskResponseModelName(c, []byte(tt.in))))
		})
	}
}

func TestMaskResponseModelNameLeavesDataUntouched(t *testing.T) {
	origin := newModelMaskContext(t, "gpt-4o")
	unmapped := newModelMaskContext(t, "")

	body := `{"model":"gpt-4o-mini-upstream"}`
	assert.Equal(t, body, MaskResponseModelNameString(unmapped, body), "未映射请求零改写")

	assert.Equal(t, "[DONE]", MaskResponseModelNameString(origin, "[DONE]"), "非 JSON 对象直通")
	assert.Equal(t, "", MaskResponseModelNameString(origin, ""), "空串直通")

	binary := string([]byte{0xFF, 0xFB, 0x90})
	assert.Equal(t, binary, MaskResponseModelNameString(origin, binary), "二进制体直通")

	noModel := `{"id":"1","choices":[]}`
	assert.Equal(t, noModel, MaskResponseModelNameString(origin, noModel), "无 model 字段直通")

	already := `{"model":"gpt-4o"}`
	assert.Equal(t, already, MaskResponseModelNameString(origin, already), "已是客户名不重写")

	nonString := `{"model":123}`
	assert.Equal(t, nonString, MaskResponseModelNameString(origin, nonString), "非字符串 model 不动")
}

func TestMaskResponseModelNameHandlesLeadingJSONWhitespace(t *testing.T) {
	c := newModelMaskContext(t, "gpt-4o")
	input := " \n\t{\"model\":\"internal-upstream-model\",\"choices\":[]}"
	want := " \n\t{\"model\":\"gpt-4o\",\"choices\":[]}"

	assert.Equal(t, want, MaskResponseModelNameString(c, input))
	assert.Equal(t, want, string(MaskResponseModelName(c, []byte(input))))
}
