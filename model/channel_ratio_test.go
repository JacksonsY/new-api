package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

func TestChannelRatioValidationAndFallback(t *testing.T) {
	valid := []float64{0, 0.5, 1, MaxChannelRatio}
	for _, ratio := range valid {
		assert.NoError(t, ValidateChannelRatio(ratio))
		assert.Equal(t, ratio, (&Channel{ChannelRatio: &ratio}).GetChannelRatio())
	}

	invalid := []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1), MaxChannelRatio + 1}
	for _, ratio := range invalid {
		assert.Error(t, ValidateChannelRatio(ratio))
		assert.Equal(t, float64(1), (&Channel{ChannelRatio: &ratio}).GetChannelRatio())
	}
}

// 渠道成本折算契约：实付先除生效分组倍率还原原始费用（折前官方口径），再乘
// 渠道倍率——成本倍率（含 sub2api/上游分组自动同步）是相对官方原价的折扣，
// 直接乘实付会在分组倍率≠1 时错估成本。分组倍率非正按 1 兜底；结果饱和防回绕。
func TestChannelCostQuotaUsesPreDiscountBase(t *testing.T) {
	tests := []struct {
		name         string
		quota        int
		groupRatio   float64
		channelRatio float64
		want         int
	}{
		{"分组折扣需还原", 500, 0.5, 0.2, 200},
		{"分组加价需还原", 2000, 2, 0.2, 200},
		{"分组倍率1等价旧口径", 1000, 1, 0.2, 200},
		{"分组倍率缺失(0)按1兜底", 1000, 0, 0.2, 200},
		{"分组倍率负值按1兜底", 1000, -3, 0.2, 200},
		{"渠道倍率0成本为0", 500, 0.5, 0, 0},
		{"四舍五入远离零", 1, 1, 0.5, 1},
		{"负额度四舍五入远离零", -1, 1, 0.5, -1},
		{"溢出饱和不回绕", common.MaxQuota, 0.5, MaxChannelRatio, common.MaxQuota},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, channelCostQuota(tt.quota, tt.groupRatio, tt.channelRatio))
		})
	}
}

// 日志 other 里的生效分组倍率提取：缺失、非 float64、非正值一律按 1 兜底，
// 保证旧格式日志与异常输入不影响渠道成本口径。
func TestGroupRatioFromLogOtherFallsBackToOne(t *testing.T) {
	assert.Equal(t, 0.5, groupRatioFromLogOther(map[string]interface{}{"group_ratio": 0.5}))
	assert.Equal(t, float64(1), groupRatioFromLogOther(nil))
	assert.Equal(t, float64(1), groupRatioFromLogOther(map[string]interface{}{}))
	assert.Equal(t, float64(1), groupRatioFromLogOther(map[string]interface{}{"group_ratio": 0.0}))
	assert.Equal(t, float64(1), groupRatioFromLogOther(map[string]interface{}{"group_ratio": -2.0}))
	assert.Equal(t, float64(1), groupRatioFromLogOther(map[string]interface{}{"group_ratio": "0.5"}))
}

func TestApplyChannelRatioSaturatesInsteadOfWrappingNegative(t *testing.T) {
	origMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = origMemoryCacheEnabled })

	ratio := MaxChannelRatio
	channelSyncLock.Lock()
	origChannels := channelsIDM
	channelsIDM = map[int]*Channel{1: {Id: 1, ChannelRatio: &ratio}}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		channelSyncLock.Lock()
		channelsIDM = origChannels
		channelSyncLock.Unlock()
	})

	assert.Equal(t, common.MaxQuota, applyChannelRatio(1, common.MaxQuota, 1))
}

// used_quota 折算走同一原始费用口径：分组倍率 0.5 的 500 实付在 0.2 倍率渠道
// 上成本为 200；渠道缓存未命中时倍率按 1，但分组倍率仍需还原。
func TestApplyChannelRatioDividesGroupRatio(t *testing.T) {
	origMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = origMemoryCacheEnabled })

	ratio := 0.2
	channelSyncLock.Lock()
	origChannels := channelsIDM
	channelsIDM = map[int]*Channel{1: {Id: 1, ChannelRatio: &ratio}}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		channelSyncLock.Lock()
		channelsIDM = origChannels
		channelSyncLock.Unlock()
	})

	assert.Equal(t, 200, applyChannelRatio(1, 500, 0.5))
	assert.Equal(t, 100, applyChannelRatio(1, 500, 1), "分组倍率 1 等价旧口径")
	assert.Equal(t, 100, applyChannelRatio(1, 500, 0), "分组倍率缺失按 1 兜底")
	assert.Equal(t, 1000, applyChannelRatio(404, 500, 0.5), "渠道缓存未命中倍率按 1，仍还原分组倍率")
}
