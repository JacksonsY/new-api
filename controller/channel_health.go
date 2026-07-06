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
		if parsed, err := strconv.Atoi(raw); err == nil {
			channelID = parsed
		}
	}
	channelhealth.Reset(channelID)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
