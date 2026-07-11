package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	channelhealth "github.com/QuantumNous/new-api/pkg/channel_health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 渠道级并发上限契约：达到 max_concurrency 的渠道被剔除；无上限渠道始终
// 通过；同层全部超限时必须拒绝，不能突破硬上限。
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

	// 候选只剩已打满的渠道：硬上限必须拒绝
	saturatedOnly := []*Channel{{Id: chSaturated}}
	assert.Empty(t, filterChannelsByConcurrencyLimit(saturatedOnly))

	// 未配置任何上限：原样返回
	channel2maxConcurrency = map[int]int{}
	assert.Len(t, filterChannelsByConcurrencyLimit(chans), 3)
}

func TestTryAcquireInflightIsAtomicAtLimit(t *testing.T) {
	const channelID = 920011
	var start sync.WaitGroup
	start.Add(1)
	results := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		go func() {
			start.Wait()
			results <- channelhealth.TryAcquireInflight(channelID, 1)
		}()
	}
	start.Done()

	first, second := <-results, <-results
	assert.NotEqual(t, first, second, "exactly one concurrent caller may claim the only slot")
	assert.EqualValues(t, 1, channelhealth.CurrentInflight(channelID))
	channelhealth.ReleaseInflight(channelID)
}

func TestSingleCandidateAtMaxConcurrencyIsUnavailable(t *testing.T) {
	const channelID = 920021
	origMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true

	channelSyncLock.Lock()
	origGroups := group2model2channels
	origChannels := channelsIDM
	origLimits := channel2maxConcurrency
	group2model2channels = map[string]map[string][]int{"default": {"test-model": {channelID}}}
	channelsIDM = map[int]*Channel{channelID: {Id: channelID}}
	channel2maxConcurrency = map[int]int{channelID: 1}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		channelhealth.ReleaseInflight(channelID)
		channelSyncLock.Lock()
		group2model2channels = origGroups
		channelsIDM = origChannels
		channel2maxConcurrency = origLimits
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = origMemoryCacheEnabled
	})

	require.True(t, TryAcquireChannelInflight(channelID))
	assert.False(t, TryAcquireChannelInflight(channelID), "a task submit cannot claim a second slot past the limit")

	channel, err := GetRandomSatisfiedChannel("default", "test-model", 0, "/v1/videos")
	require.NoError(t, err)
	assert.Nil(t, channel, "the single-candidate fast path must honor max_concurrency")
}

func TestSelectionExcludesChannelThatLosesInflightRace(t *testing.T) {
	const (
		firstChannelID  = 920031
		secondChannelID = 920032
	)
	origMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true

	channelSyncLock.Lock()
	origGroups := group2model2channels
	origChannels := channelsIDM
	origLimits := channel2maxConcurrency
	group2model2channels = map[string]map[string][]int{
		"default": {"race-model": {firstChannelID, secondChannelID}},
	}
	channelsIDM = map[int]*Channel{
		firstChannelID:  {Id: firstChannelID},
		secondChannelID: {Id: secondChannelID},
	}
	channel2maxConcurrency = map[int]int{firstChannelID: 1, secondChannelID: 1}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		channelhealth.ReleaseInflight(firstChannelID)
		channelhealth.ReleaseInflight(secondChannelID)
		channelSyncLock.Lock()
		group2model2channels = origGroups
		channelsIDM = origChannels
		channel2maxConcurrency = origLimits
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = origMemoryCacheEnabled
	})

	selected, err := GetRandomSatisfiedChannel("default", "race-model", 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, selected)

	// A competing request claims the slot after the optimistic selection
	// filter, so this request must lose the atomic acquisition without
	// exceeding the hard limit.
	require.True(t, channelhealth.TryAcquireInflight(selected.Id, 1))
	assert.False(t, TryAcquireChannelInflight(selected.Id))
	assert.EqualValues(t, 1, channelhealth.CurrentInflight(selected.Id))

	excluded := map[int]struct{}{selected.Id: {}}
	reselected, err := GetRandomSatisfiedChannelExcluding("default", "race-model", 0, "/v1/chat/completions", excluded)
	require.NoError(t, err)
	require.NotNil(t, reselected)
	assert.NotEqual(t, selected.Id, reselected.Id)
	require.True(t, TryAcquireChannelInflight(reselected.Id))
	assert.EqualValues(t, 1, channelhealth.CurrentInflight(reselected.Id))
}

func TestValidateSettingsRejectsNegativeMaxConcurrency(t *testing.T) {
	negative := `{"max_concurrency":-1}`
	assert.Error(t, (&Channel{Setting: &negative}).ValidateSettings())

	unlimited := `{"max_concurrency":0}`
	assert.NoError(t, (&Channel{Setting: &unlimited}).ValidateSettings())
}
