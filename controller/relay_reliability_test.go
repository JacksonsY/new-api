package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	channelhealth "github.com/QuantumNous/new-api/pkg/channel_health"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRelayReliabilityTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return ctx
}

func withReliabilitySetting(t *testing.T, mutate func(*operation_setting.ReliabilitySetting)) {
	t.Helper()
	rs := operation_setting.GetReliabilitySetting()
	orig := *rs
	t.Cleanup(func() { *rs = orig })
	if mutate != nil {
		mutate(rs)
	}
}

func withAdaptiveRoutingEnabled(t *testing.T) {
	t.Helper()
	ar := operation_setting.GetAdaptiveRoutingSetting()
	orig := *ar
	t.Cleanup(func() { *ar = orig })
	ar.Enabled = true
	ar.CircuitEnabled = true
}

func TestShouldFastRetrySameChannel(t *testing.T) {
	withReliabilitySetting(t, func(rs *operation_setting.ReliabilitySetting) {
		rs.SameChannelRetryEnabled = true
		rs.SameChannelRetryTimes = 1
	})

	netErr := types.NewOpenAIError(errors.New("connection reset by peer"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	upstreamErr := types.NewErrorWithStatusCode(errors.New("bad gateway"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)

	t.Run("network send failure qualifies", func(t *testing.T) {
		c := newRelayReliabilityTestContext(t)
		assert.True(t, shouldFastRetrySameChannel(c, netErr, 0))
	})

	t.Run("budget exhausted", func(t *testing.T) {
		c := newRelayReliabilityTestContext(t)
		assert.False(t, shouldFastRetrySameChannel(c, netErr, 1))
	})

	t.Run("upstream http error does not qualify", func(t *testing.T) {
		c := newRelayReliabilityTestContext(t)
		assert.False(t, shouldFastRetrySameChannel(c, upstreamErr, 0))
	})

	t.Run("specific channel debug request skipped", func(t *testing.T) {
		c := newRelayReliabilityTestContext(t)
		c.Set("specific_channel_id", 1)
		assert.False(t, shouldFastRetrySameChannel(c, netErr, 0))
	})

	t.Run("disabled setting", func(t *testing.T) {
		withReliabilitySetting(t, func(rs *operation_setting.ReliabilitySetting) {
			rs.SameChannelRetryEnabled = false
		})
		c := newRelayReliabilityTestContext(t)
		assert.False(t, shouldFastRetrySameChannel(c, netErr, 0))
	})
}

func TestMaybeApplyRateLimitCooldown(t *testing.T) {
	withAdaptiveRoutingEnabled(t)
	withReliabilitySetting(t, func(rs *operation_setting.ReliabilitySetting) {
		rs.RateLimitCooldownEnabled = true
		rs.RateLimitCooldownDefaultSeconds = 30
		rs.RateLimitCooldownMaxSeconds = 1800
	})

	newRateLimitErr := func(resetAfter time.Duration) *types.NewAPIError {
		err := types.NewErrorWithStatusCode(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
		err.SetRateLimitResetAfter(resetAfter)
		return err
	}

	t.Run("upstream reset time drives cooldown", func(t *testing.T) {
		const chID = 930001
		c := newRelayReliabilityTestContext(t)
		maybeApplyRateLimitCooldown(c, &model.Channel{Id: chID}, newRateLimitErr(45*time.Second))

		view, ok := channelhealth.GetStatView(chID)
		require.True(t, ok)
		assert.True(t, view.CircuitOpen)
		assert.InDelta(t, 45_000, view.CooldownMs, 2_000)
	})

	t.Run("missing reset falls back to default", func(t *testing.T) {
		const chID = 930002
		c := newRelayReliabilityTestContext(t)
		maybeApplyRateLimitCooldown(c, &model.Channel{Id: chID}, newRateLimitErr(0))

		view, ok := channelhealth.GetStatView(chID)
		require.True(t, ok)
		assert.True(t, view.CircuitOpen)
		assert.InDelta(t, 30_000, view.CooldownMs, 2_000)
	})

	t.Run("reset clamped to max", func(t *testing.T) {
		const chID = 930003
		c := newRelayReliabilityTestContext(t)
		maybeApplyRateLimitCooldown(c, &model.Channel{Id: chID}, newRateLimitErr(3*time.Hour))

		view, ok := channelhealth.GetStatView(chID)
		require.True(t, ok)
		assert.True(t, view.CircuitOpen)
		assert.LessOrEqual(t, view.CooldownMs, int64(1800_000))
		assert.InDelta(t, 1800_000, view.CooldownMs, 5_000)
	})

	t.Run("multi key channel skipped", func(t *testing.T) {
		const chID = 930004
		c := newRelayReliabilityTestContext(t)
		ch := &model.Channel{Id: chID}
		ch.ChannelInfo.IsMultiKey = true
		maybeApplyRateLimitCooldown(c, ch, newRateLimitErr(45*time.Second))

		if view, ok := channelhealth.GetStatView(chID); ok {
			assert.False(t, view.CircuitOpen)
		}
	})

	t.Run("non-429 skipped", func(t *testing.T) {
		const chID = 930005
		c := newRelayReliabilityTestContext(t)
		err := types.NewErrorWithStatusCode(errors.New("server error"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
		maybeApplyRateLimitCooldown(c, &model.Channel{Id: chID}, err)

		if view, ok := channelhealth.GetStatView(chID); ok {
			assert.False(t, view.CircuitOpen)
		}
	})

	t.Run("cooldown disabled", func(t *testing.T) {
		withReliabilitySetting(t, func(rs *operation_setting.ReliabilitySetting) {
			rs.RateLimitCooldownEnabled = false
		})
		const chID = 930006
		c := newRelayReliabilityTestContext(t)
		maybeApplyRateLimitCooldown(c, &model.Channel{Id: chID}, newRateLimitErr(45*time.Second))

		if view, ok := channelhealth.GetStatView(chID); ok {
			assert.False(t, view.CircuitOpen)
		}
	})
}
