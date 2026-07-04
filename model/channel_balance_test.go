package model

// 蓝图A 渠道余额告警的账务口径测试：快照打点、活跃日聚合、24h 滑窗。

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateBalanceStampsSnapshot 余额落库必须同步打 used_quota 快照——
// 实时余额 = balance - (used_quota - snapshot)/QuotaPerUnit 的推算基石。
func TestUpdateBalanceStampsSnapshot(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Channel{}))
	ch := &Channel{Name: "balance_snap_ch", Type: 1, Key: "sk-test", UsedQuota: 12345}
	require.NoError(t, DB.Create(ch).Error)

	ch.UpdateBalance(9.5)

	var reloaded Channel
	require.NoError(t, DB.First(&reloaded, ch.Id).Error)
	assert.Equal(t, 9.5, reloaded.Balance)
	require.NotNil(t, reloaded.BalanceSnapshot, "UpdateBalance 必须打快照")
	assert.EqualValues(t, 12345, *reloaded.BalanceSnapshot)

	// 手动设余额同样打快照
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", ch.Id).
		Update("used_quota", 20000).Error)
	require.NoError(t, SetChannelBalanceManually(ch.Id, 5))
	require.NoError(t, DB.First(&reloaded, ch.Id).Error)
	assert.Equal(t, float64(5), reloaded.Balance)
	require.NotNil(t, reloaded.BalanceSnapshot)
	assert.EqualValues(t, 20000, *reloaded.BalanceSnapshot)

	assert.Error(t, SetChannelBalanceManually(999999, 1), "不存在的渠道应报错")
}

// TestGetChannelsRecentUsage 活跃日口径：只取最近 maxActiveDays 个"有消费的日子"，
// 中间的零消费日不稀释日均；不同渠道互不影响；窗口外数据不计。
func TestGetChannelsRecentUsage(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	now := common.GetTimestamp()
	dayStart := now - now%86400

	chA, chB := 91001, 91002
	mkLog := func(channelId int, offsetDays int, quota int, logType int) *Log {
		return &Log{
			UserId: 1, Type: logType, ChannelId: channelId, Quota: quota,
			CreatedAt: dayStart - int64(offsetDays)*86400 + 100,
		}
	}
	// chA：今天 100、昨天 200、5 天前 300（中间空洞）、95 天前 999（窗口外）
	require.NoError(t, DB.Create(mkLog(chA, 0, 100, LogTypeConsume)).Error)
	require.NoError(t, DB.Create(mkLog(chA, 1, 200, LogTypeConsume)).Error)
	require.NoError(t, DB.Create(mkLog(chA, 5, 300, LogTypeConsume)).Error)
	require.NoError(t, DB.Create(mkLog(chA, 95, 999, LogTypeConsume)).Error)
	// 非消费类型不计
	require.NoError(t, DB.Create(mkLog(chA, 0, 5000, LogTypeTopup)).Error)
	// chB：只有今天 50
	require.NoError(t, DB.Create(mkLog(chB, 0, 50, LogTypeConsume)).Error)

	since := now - int64(ChannelRecentUsageLookbackDays)*86400
	usage, err := GetChannelsRecentUsage([]int{chA, chB}, since, ChannelRecentUsageActiveDays)
	require.NoError(t, err)

	require.Contains(t, usage, chA)
	assert.EqualValues(t, 600, usage[chA].Quota, "三个活跃日的消费求和，窗口外与非消费类型不计")
	assert.Equal(t, 3, usage[chA].ActiveDays)
	require.Contains(t, usage, chB)
	assert.EqualValues(t, 50, usage[chB].Quota)
	assert.Equal(t, 1, usage[chB].ActiveDays)

	// maxActiveDays=2：chA 只取最近两个活跃日（今天+昨天）
	usage, err = GetChannelsRecentUsage([]int{chA}, since, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 300, usage[chA].Quota)
	assert.Equal(t, 2, usage[chA].ActiveDays)
}

// TestGetChannelsQuotaSince 24h 滑窗口径：不按日分桶，窗口外不计，无消费渠道缺席。
func TestGetChannelsQuotaSince(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	now := common.GetTimestamp()
	ch := 92001
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: ch, Quota: 70, CreatedAt: now - 3600,
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: ch, Quota: 999, CreatedAt: now - 90000, // >24h
	}).Error)

	result, err := GetChannelsQuotaSince([]int{ch, 92002}, now-86400)
	require.NoError(t, err)
	assert.EqualValues(t, 70, result[ch])
	_, ok := result[92002]
	assert.False(t, ok, "窗口内无消费的渠道不应出现在结果里")
}
