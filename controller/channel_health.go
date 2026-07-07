package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	channelhealth "github.com/QuantumNous/new-api/pkg/channel_health"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// channelHealthRow enriches a live StatView with the channel's identity
// (name/type/group/status) so the ops-monitor page can render human-readable
// channels instead of bare IDs. The embedded StatView keeps its JSON fields
// flat. Metadata is a best-effort cache lookup: a channel deleted while it still
// holds in-memory health stats simply renders with empty metadata.
type channelHealthRow struct {
	channelhealth.StatView
	Name   string `json:"name"`
	Type   int    `json:"type"`
	Group  string `json:"group"`
	Status int    `json:"status"`
	// TestLatencyMs / TestTime are the channel's last test result (manual test
	// or the scheduled auto-test), reused as a zero-cost "endpoint ping" — no
	// extra probe is issued for this page.
	TestLatencyMs int   `json:"test_latency_ms"`
	TestTime      int64 `json:"test_time"`
	// Balance is the locally-estimated live balance (蓝图A: no upstream call);
	// HasBalance is false for channels that never reported a balance.
	Balance            float64 `json:"balance"`
	HasBalance         bool    `json:"has_balance"`
	BalanceUpdatedTime int64   `json:"balance_updated_time"`
}

// GetChannelHealth returns the live per-channel adaptive-routing health for
// every channel this instance has observed: first-token-latency and error-rate
// EWMA, in-flight count, and circuit-breaker state, each joined with the
// channel's name/type/group/status. Admin-only, since it exposes internal
// routing state. Counts are per-instance (this replica's own view).
func GetChannelHealth(c *gin.Context) {
	views := channelhealth.AllStatViews()
	rows := make([]channelHealthRow, 0, len(views))
	for _, v := range views {
		row := channelHealthRow{StatView: v}
		if ch, err := model.CacheGetChannel(v.ChannelID); err == nil && ch != nil {
			row.Name = ch.Name
			row.Type = ch.Type
			row.Group = ch.Group
			row.Status = ch.Status
			row.TestLatencyMs = ch.ResponseTime
			row.TestTime = ch.TestTime
			// Live balance = last upstream balance minus quota spent since the
			// snapshot was taken; no upstream call is made.
			liveBalance := ch.Balance
			if ch.BalanceSnapshot != nil {
				liveBalance -= float64(ch.UsedQuota-*ch.BalanceSnapshot) / common.QuotaPerUnit
			}
			row.Balance = liveBalance
			row.HasBalance = ch.BalanceSnapshot != nil || ch.Balance != 0
			row.BalanceUpdatedTime = ch.BalanceUpdatedTime
		}
		rows = append(rows, row)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":  operation_setting.GetAdaptiveRoutingSetting().Enabled,
			"channels": rows,
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
