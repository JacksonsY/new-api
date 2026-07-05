package service

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 非流式出口的模型名改写必须发生在 Content-Length 计算之前，否则响应体被截断/超长。
func TestIOCopyBytesGracefullyMasksModelAndKeepsContentLengthConsistent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyClientFacingModelName, "gpt-4o")

	body := []byte(`{"id":"chatcmpl-1","model":"gpt-4o-mini-upstream-secret","choices":[]}`)
	IOCopyBytesGracefully(c, nil, body)

	written := recorder.Body.String()
	assert.Contains(t, written, `"model":"gpt-4o"`)
	assert.NotContains(t, written, "upstream-secret")

	contentLength, err := strconv.Atoi(recorder.Header().Get("Content-Length"))
	require.NoError(t, err)
	assert.Equal(t, len(written), contentLength, "Content-Length 必须与改写后的响应体一致")
}
