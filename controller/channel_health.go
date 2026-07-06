package controller

import (
	"net/http"
	"strconv"

	channelhealth "github.com/QuantumNous/new-api/pkg/channel_health"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// GetChannelHealth returns the live per-channel adaptive-routing health for
// every channel this instance has observed: first-token-latency and error-rate
// EWMA, in-flight count, and circuit-breaker state. Admin-only, since it exposes
// internal routing state. Counts are per-instance (this replica's own view).
func GetChannelHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":  operation_setting.GetAdaptiveRoutingSetting().Enabled,
			"channels": channelhealth.AllStatViews(),
		},
	})
}

// ResetChannelHealth clears a channel's learned health and circuit trip (local +
// cluster-wide). Query channel_id selects one channel; all=true (or no id)
// resets every channel. Admin-only, for forcing recovery after fixing upstream.
func ResetChannelHealth(c *gin.Context) {
	channelID := 0
	if raw := c.Query("channel_id"); raw != "" {
		// A malformed channel_id must NOT silently fall through to channelID=0,
		// which is the "reset everything" path — reject it so a typo can't wipe
		// every channel's health and open circuit cluster-wide.
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel_id"})
			return
		}
		channelID = parsed
	}
	channelhealth.Reset(channelID)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
