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
			ChannelRatio: 1,
			CreatedAt:    dayStart - int64(offsetDays)*86400 + 100,
		}
	}
	// chA：今天 100、昨天 200、5 天前 300（中间空洞）、95 天前 999（窗口外）
	require.NoError(t, DB.Create(mkLog(chA, 0, 100, LogTypeConsume)).Error)
	require.NoError(t, DB.Create(mkLog(chA, 1, 200, LogTypeConsume)).Error)
	require.NoError(t, DB.Create(mkLog(chA, 5, 300, LogTypeConsume)).Error)
	// 逐笔倍率必须在聚合时保留：今天 0.5 倍、昨天 2 倍、5 天前 1 倍。
	require.NoError(t, DB.Model(&Log{}).Where("channel_id = ? AND created_at = ?", chA, dayStart+100).Update("channel_ratio", 0.5).Error)
	require.NoError(t, DB.Model(&Log{}).Where("channel_id = ? AND created_at = ?", chA, dayStart-86400+100).Update("channel_ratio", 2.0).Error)
	require.NoError(t, DB.Create(mkLog(chA, 95, 999, LogTypeConsume)).Error)
	// 非消费类型不计
	require.NoError(t, DB.Create(mkLog(chA, 0, 5000, LogTypeTopup)).Error)
	// chB：只有今天 50
	require.NoError(t, DB.Create(mkLog(chB, 0, 50, LogTypeConsume)).Error)

	since := now - int64(ChannelRecentUsageLookbackDays)*86400
	usage, err := GetChannelsRecentUsage([]int{chA, chB}, since, ChannelRecentUsageActiveDays)
	require.NoError(t, err)

	require.Contains(t, usage, chA)
	assert.EqualValues(t, 750, usage[chA].Quota, "逐笔按历史倍率折算，窗口外与非消费类型不计")
	assert.Equal(t, 3, usage[chA].ActiveDays)
	require.Contains(t, usage, chB)
	assert.EqualValues(t, 50, usage[chB].Quota)
	assert.Equal(t, 1, usage[chB].ActiveDays)

	// maxActiveDays=2：chA 只取最近两个活跃日（今天+昨天）
	usage, err = GetChannelsRecentUsage([]int{chA}, since, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 450, usage[chA].Quota)
	assert.Equal(t, 2, usage[chA].ActiveDays)
}

// TestGetChannelsQuotaSince 24h 滑窗口径：不按日分桶，窗口外不计，无消费渠道缺席。
func TestGetChannelsQuotaSince(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	now := common.GetTimestamp()
	ch := 92001
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: ch, Quota: 70, ChannelRatio: 2, CreatedAt: now - 3600,
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: ch, Quota: 20, ChannelRatio: 0.5, CreatedAt: now - 1800,
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: ch, Quota: 999, ChannelRatio: 1, CreatedAt: now - 90000, // >24h
	}).Error)

	result, err := GetChannelsQuotaSince([]int{ch, 92002}, now-86400)
	require.NoError(t, err)
	assert.EqualValues(t, 150, result[ch])
	_, ok := result[92002]
	assert.False(t, ok, "窗口内无消费的渠道不应出现在结果里")
}

func TestGetChannelsUsageSaturatesEachHistoricalEntry(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	now := common.GetTimestamp()
	dayStart := now - now%86400
	const overflowChannel = 92011
	const underflowChannel = 92012

	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: overflowChannel,
		Quota: common.MaxQuota, ChannelRatio: MaxChannelRatio, CreatedAt: dayStart + 100,
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: underflowChannel,
		Quota: common.MinQuota, ChannelRatio: MaxChannelRatio, CreatedAt: dayStart + 100,
	}).Error)

	recent, err := GetChannelsRecentUsage(
		[]int{overflowChannel, underflowChannel}, dayStart, ChannelRecentUsageActiveDays,
	)
	require.NoError(t, err)
	assert.EqualValues(t, common.MaxQuota, recent[overflowChannel].Quota)
	assert.EqualValues(t, common.MinQuota, recent[underflowChannel].Quota)

	since, err := GetChannelsQuotaSince([]int{overflowChannel, underflowChannel}, dayStart)
	require.NoError(t, err)
	assert.EqualValues(t, common.MaxQuota, since[overflowChannel])
	assert.EqualValues(t, common.MinQuota, since[underflowChannel])
}

func TestGetChannelsUsageRoundsHalfAwayFromZero(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	now := common.GetTimestamp()
	const positiveChannel = 92021
	const negativeChannel = 92022

	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: positiveChannel,
		Quota: 1, ChannelRatio: 0.5, CreatedAt: now,
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: negativeChannel,
		Quota: -1, ChannelRatio: 0.5, CreatedAt: now,
	}).Error)

	usage, err := GetChannelsQuotaSince([]int{positiveChannel, negativeChannel}, now-1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, usage[positiveChannel])
	assert.EqualValues(t, -1, usage[negativeChannel])
}

// 渠道支出聚合优先读 channel_quota 快照（原始费用口径：实付÷生效分组倍率×渠道
// 倍率）；加列前的旧行（快照 0/NULL）回退 quota×channel_ratio 旧口径，两代数据
// 混存时各自口径成立、可直接相加。
func TestGetChannelsUsagePrefersChannelQuotaSnapshot(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	now := common.GetTimestamp()
	const ch = 92041

	// 新行：实付 500、分组倍率 0.5、渠道倍率 0.2 → 快照 500÷0.5×0.2 = 200
	// （若错用实付基数会得 100，聚合总额将暴露回归）
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: ch,
		Quota: 500, ChannelRatio: 0.2, ChannelRatioSet: true, ChannelQuota: 200,
		CreatedAt: now - 60,
	}).Error)
	// 旧行：无快照，回退 100×0.2 = 20
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: ch,
		Quota: 100, ChannelRatio: 0.2, ChannelRatioSet: true,
		CreatedAt: now - 30,
	}).Error)

	usage, err := GetChannelsQuotaSince([]int{ch}, now-3600)
	require.NoError(t, err)
	assert.EqualValues(t, 220, usage[ch], "新行读快照(200)，旧行回退旧口径(20)")

	daily, err := GetChannelsRecentUsage([]int{ch}, now-3600, ChannelRecentUsageActiveDays)
	require.NoError(t, err)
	assert.EqualValues(t, 220, daily[ch].Quota, "按日聚合与滑窗聚合同一口径")
}

func TestGetChannelsUsageDistinguishesLegacyAndExplicitZeroRatio(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	now := common.GetTimestamp()
	const legacyChannel = 92031
	const explicitZeroChannel = 92032

	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: legacyChannel,
		Quota: 100, ChannelRatio: 0, ChannelRatioSet: false, CreatedAt: now,
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ChannelId: explicitZeroChannel,
		Quota: 100, ChannelRatio: 0, ChannelRatioSet: true, CreatedAt: now,
	}).Error)

	usage, err := GetChannelsQuotaSince([]int{legacyChannel, explicitZeroChannel}, now-1)
	require.NoError(t, err)
	assert.EqualValues(t, 100, usage[legacyChannel], "pre-snapshot rows must retain their original 1x cost")
	assert.EqualValues(t, 0, usage[explicitZeroChannel], "new explicit zero-cost snapshots must remain zero")
}
