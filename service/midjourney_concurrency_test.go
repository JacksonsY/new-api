package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	channelhealth "github.com/QuantumNous/new-api/pkg/channel_health"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoMidjourneyHttpRequestHonorsChannelConcurrencyLimit(t *testing.T) {
	const channelID = 940101
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	settings := `{"max_concurrency":1}`
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      channelID,
		Name:    "midjourney-concurrency-test",
		Status:  common.ChannelStatusEnabled,
		Setting: &settings,
	}).Error)
	t.Cleanup(func() { _ = model.DB.Delete(&model.Channel{}, channelID).Error })

	require.True(t, model.TryAcquireChannelInflight(channelID))
	t.Cleanup(func() { channelhealth.ReleaseInflight(channelID) })

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{}`))
	ctx.Set("channel_id", channelID)

	response, _, err := DoMidjourneyHttpRequest(ctx, 0, upstream.URL)
	require.Error(t, err)
	require.NotNil(t, response)
	assert.Equal(t, http.StatusTooManyRequests, response.StatusCode)
	assert.Equal(t, constant.MjConcurrencyError, response.Response.Code)
	assert.Zero(t, upstreamCalls.Load(), "saturated legacy task submissions must fail before reaching upstream")
	assert.EqualValues(t, 1, channelhealth.CurrentInflight(channelID))
}
