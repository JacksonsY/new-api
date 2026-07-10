package model

import (
	"testing"

	channelhealth "github.com/QuantumNous/new-api/pkg/channel_health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 渠道级并发上限契约：达到 max_concurrency 的渠道被剔除；无上限渠道始终
// 通过；同层全部超限时放行原集合（fail-open）。
func TestFilterChannelsByConcurrencyLimit(t *testing.T) {
	origLimits := channel2maxConcurrency
	t.Cleanup(func() { channel2maxConcurrency = origLimits })

	const chLimited, chSaturated, chUnlimited = 920001, 920002, 920003
	channel2maxConcurrency = map[int]int{chLimited: 2, chSaturated: 1}

	chans := []*Channel{{Id: chLimited}, {Id: chSaturated}, {Id: chUnlimited}}

	// 无在飞请求：全部通过
	require.Len(t, filterChannelsByConcurrencyLimit(chans), 3)

	// chSaturated 打满（1/1），chLimited 未满（1/2）
	channelhealth.AcquireInflight(chSaturated)
	channelhealth.AcquireInflight(chLimited)
	t.Cleanup(func() {
		channelhealth.ReleaseInflight(chSaturated)
		channelhealth.ReleaseInflight(chLimited)
	})

	filtered := filterChannelsByConcurrencyLimit(chans)
	require.Len(t, filtered, 2)
	ids := []int{filtered[0].Id, filtered[1].Id}
	assert.ElementsMatch(t, []int{chLimited, chUnlimited}, ids)

	// 候选只剩已打满的渠道：fail-open 放行
	saturatedOnly := []*Channel{{Id: chSaturated}}
	assert.Len(t, filterChannelsByConcurrencyLimit(saturatedOnly), 1)

	// 未配置任何上限：原样返回
	channel2maxConcurrency = map[int]int{}
	assert.Len(t, filterChannelsByConcurrencyLimit(chans), 3)
}
