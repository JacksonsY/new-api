package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 验证 fork 适配核心：DecreaseChannelUsedQuota 与 UpdateChannelUsedQuota 走同一
// applyChannelRatio 折算（非 1 渠道倍率 + 非 1 分组倍率），正反向成本口径对称、
// 过度回退钳 0。channelCostQuota((quota/groupRatio)*channelRatio)。
func TestChannelUsedQuotaRatioFoldingSymmetric(t *testing.T) {
	originalBatch := common.BatchUpdateEnabled
	originalMemCache := common.MemoryCacheEnabled
	common.BatchUpdateEnabled = false // 直接落库，不走 batch flush
	common.MemoryCacheEnabled = false // CacheGetChannel 回落 DB 读到 ChannelRatio
	t.Cleanup(func() {
		common.BatchUpdateEnabled = originalBatch
		common.MemoryCacheEnabled = originalMemCache
		require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	})
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	ratio := 2.0
	ch := &Channel{
		Id:           501,
		Name:         "ratio-channel",
		Key:          "sk-ratio",
		Status:       common.ChannelStatusEnabled,
		ChannelRatio: &ratio,
	}
	require.NoError(t, DB.Create(ch).Error)

	// 正向：成本口径 = (100 / 0.5) * 2.0 = 400
	UpdateChannelUsedQuota(501, 100, 0.5)
	require.EqualValues(t, 400, channelUsedQuota(t, 501))

	// 反向同口径回退 → 归零（对称）
	DecreaseChannelUsedQuota(501, 100, 0.5)
	require.EqualValues(t, 0, channelUsedQuota(t, 501))

	// 过度回退钳 0（并发/重复退款不产生负值）
	DecreaseChannelUsedQuota(501, 100, 0.5)
	require.EqualValues(t, 0, channelUsedQuota(t, 501))

	// 非法入参早返回，不改动
	UpdateChannelUsedQuota(501, 100, 0.5) // 回到 400
	DecreaseChannelUsedQuota(501, 0, 0.5)
	DecreaseChannelUsedQuota(0, 100, 0.5)
	require.EqualValues(t, 400, channelUsedQuota(t, 501))
}

func channelUsedQuota(t *testing.T, id int) int64 {
	t.Helper()
	var ch Channel
	require.NoError(t, DB.Select("used_quota").First(&ch, id).Error)
	return ch.UsedQuota
}
