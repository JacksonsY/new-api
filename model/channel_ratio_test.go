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

	assert.Equal(t, common.MaxQuota, applyChannelRatio(1, common.MaxQuota))
}
