package gemini

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyAppliesVeoMetadataAndAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos/generations", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt: "interpolate the frames",
		Metadata: map[string]any{
			"image": map[string]any{
				"inlineData": map[string]any{
					"mimeType": "image/png",
					"data":     "first-base64",
				},
			},
			"lastFrame": map[string]any{
				"inlineData": map[string]any{
					"mimeType": "image/png",
					"data":     "last-base64",
				},
			},
		},
	})
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionTextGenerate}}

	reader, err := (&TaskAdaptor{}).BuildRequestBody(c, info)

	require.NoError(t, err)
	var payload VeoRequestPayload
	require.NoError(t, common.DecodeJson(reader, &payload))
	require.Len(t, payload.Instances, 1)
	require.Equal(t, "first-base64", payload.Instances[0].Image.BytesBase64Encoded)
	require.Equal(t, "last-base64", payload.Instances[0].LastFrame.BytesBase64Encoded)
	require.Equal(t, constant.TaskActionFirstTailGenerate, info.Action)
}
