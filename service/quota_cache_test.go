package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Realtime/音频计费的缓存折扣契约:cached 是输入(text+audio)的子集,
// 先抵扣文本侧、溢出计入音频侧(乘 audioRatio),超出总输入的部分丢弃;
// CacheRatio<=0 视为未配置按全价——上游回报的缓存数不得把配额抵成负数,
// 未接线调用方的零值结构不得意外免单。
func TestCalculateAudioQuotaCacheDiscount(t *testing.T) {
	const model = "quota-cache-test-model"

	prevCompletion := ratio_setting.CompletionRatio2JSONString()
	prevAudio := ratio_setting.AudioRatio2JSONString()
	prevAudioCompletion := ratio_setting.AudioCompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(prevCompletion))
		require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(prevAudio))
		require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(prevAudioCompletion))
	})
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"`+model+`":2}`))
	require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(`{"`+model+`":10}`))
	require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(`{"`+model+`":3}`))

	baseInfo := func() QuotaInfo {
		return QuotaInfo{
			InputDetails:  TokenDetails{TextTokens: 1000, AudioTokens: 500},
			OutputDetails: TokenDetails{TextTokens: 100, AudioTokens: 50},
			ModelName:     model,
			ModelRatio:    1,
			GroupRatio:    1,
		}
	}
	// 基线:1000 + 100*2 + 500*10 + 50*10*3 = 7700
	const baseline = 7700

	tests := []struct {
		name         string
		cachedTokens int
		cacheRatio   float64
		want         int
	}{
		{"无缓存不变", 0, 0.5, baseline},
		{"缓存优先抵扣文本侧", 400, 0.5, 7500},          // 600 + 400*0.5 = 800(省 200)
		{"溢出部分计入音频侧", 1200, 0.5, 6200},         // text: 1000*0.5=500; audio: (300+200*0.5)*10=4000
		{"超过总输入的缓存数被丢弃(防负)", 99999, 0.5, 4700}, // text: 500; audio: 500*0.5*10=2500
		{"CacheRatio=1 等同无折扣", 400, 1, baseline},
		{"CacheRatio<=0 视为未配置按全价", 400, 0, baseline}, // 零值结构防误免单
		{"负缓存数忽略", -5, 0.5, baseline},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := baseInfo()
			info.InputDetails.CachedTokens = tt.cachedTokens
			info.CacheRatio = tt.cacheRatio
			quota, clamp := calculateAudioQuota(info)
			assert.Nil(t, clamp)
			assert.Equal(t, tt.want, quota)
		})
	}
}

// 纯音频会话的缓存也要打折(现实场景:Realtime 语音对话 cached 多在音频侧)。
func TestCalculateAudioQuotaCacheDiscountAudioOnly(t *testing.T) {
	const model = "quota-cache-test-model-audio"
	prevAudio := ratio_setting.AudioRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(prevAudio))
	})
	require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(`{"`+model+`":20}`))

	info := QuotaInfo{
		InputDetails: TokenDetails{TextTokens: 0, AudioTokens: 1000, CachedTokens: 600},
		ModelName:    model,
		ModelRatio:   1,
		GroupRatio:   1,
		CacheRatio:   0.2,
	}
	// 文本侧无可抵扣,600 全部计入音频侧:400*20 + 600*0.2*20 = 8000 + 2400
	quota, clamp := calculateAudioQuota(info)
	assert.Nil(t, clamp)
	assert.Equal(t, 10400, quota)
}
