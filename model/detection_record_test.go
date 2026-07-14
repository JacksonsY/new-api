package model

// jzlh-veridrop 红黑榜聚合 SQL 的跨库正确性回归（在 SQLite 上验证 CASE/AVG/MIN/GROUP BY）。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectionLeaderboardAggregates(t *testing.T) {
	restore := swapToFreshSupplierDB(t, &DetectionRecord{})
	defer restore()

	seed := []DetectionRecord{
		{Domain: "bad.com", Verdict: "failed", Score: 20, CriticalCount: 1, CreatedAt: 100},
		{Domain: "bad.com", Verdict: "marginal", Score: 40, CriticalCount: 0, CreatedAt: 200},
		{Domain: "good.com", Verdict: "passed", Score: 95, CriticalCount: 0, CreatedAt: 300},
		{Domain: "", Verdict: "passed", Score: 88, CriticalCount: 0, CreatedAt: 400}, // 空域名不上榜
	}
	for i := range seed {
		require.NoError(t, seed[i].Insert())
	}

	entries, err := GetDetectionLeaderboard(0, 50)
	require.NoError(t, err)
	require.Len(t, entries, 2, "空域名应被排除")

	// 分低者排前（黑榜优先）
	assert.Equal(t, "bad.com", entries[0].Domain)
	assert.Equal(t, int64(2), entries[0].Samples)
	assert.InDelta(t, 30.0, entries[0].AvgScore, 0.001) // (20+40)/2
	assert.InDelta(t, 20.0, entries[0].MinScore, 0.001)
	assert.Equal(t, int64(1), entries[0].CriticalCount) // 一条 critical
	assert.Equal(t, int64(200), entries[0].LastCheckedAt)

	assert.Equal(t, "good.com", entries[1].Domain)
	assert.Equal(t, int64(1), entries[1].Samples)
	assert.InDelta(t, 95.0, entries[1].AvgScore, 0.001)
}
