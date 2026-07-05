package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 重试换渠道后改写状态必须跟着换：映射渠道设置客户名，换到无映射渠道必须清零，
// 否则会把上一渠道的名字写进本渠道的响应。
func TestModelMappedHelperSetsAndResetsMaskState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &common.RelayInfo{
		OriginModelName: "gpt-4o",
		ChannelMeta:     &common.ChannelMeta{UpstreamModelName: "gpt-4o"},
	}

	// 第一次尝试：命中映射渠道
	c.Set("model_mapping", `{"gpt-4o":"gpt-4o-mini-upstream"}`)
	require.NoError(t, ModelMappedHelper(c, info, nil))
	require.True(t, info.IsModelMapped)
	assert.Equal(t, "gpt-4o", basecommon.GetContextKeyString(c, constant.ContextKeyClientFacingModelName))

	// 重试：换到无映射渠道（ChannelMeta 按次重建）
	info.ChannelMeta = &common.ChannelMeta{UpstreamModelName: "gpt-4o"}
	c.Set("model_mapping", "")
	require.NoError(t, ModelMappedHelper(c, info, nil))
	assert.Empty(t, basecommon.GetContextKeyString(c, constant.ContextKeyClientFacingModelName),
		"无映射渠道必须清零改写状态")

	// 自映射（映射回自身）同样不改写
	info.ChannelMeta = &common.ChannelMeta{UpstreamModelName: "gpt-4o"}
	c.Set("model_mapping", `{"gpt-4o":"gpt-4o"}`)
	require.NoError(t, ModelMappedHelper(c, info, nil))
	assert.Empty(t, basecommon.GetContextKeyString(c, constant.ContextKeyClientFacingModelName))
}
