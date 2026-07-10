package channelhealth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 429 冷却契约：ForceOpenUntil 把渠道熔断到指定时刻，被 Select 排除；
// 全部候选被排除时 fail-open（ok=false 走 legacy 选路）；只延长不缩短。
func TestForceOpenUntilExcludesFromSelect(t *testing.T) {
	configureAdaptive(t)
	const chCooling, chHealthy = 910101, 910102

	ForceOpenUntil(chCooling, time.Now().Add(time.Minute))

	view, ok := GetStatView(chCooling)
	require.True(t, ok)
	assert.True(t, view.CircuitOpen)
	assert.Positive(t, view.CooldownMs)

	for i := 0; i < 20; i++ {
		id, picked := Select([]Candidate{{ChannelID: chCooling, Weight: 100}, {ChannelID: chHealthy, Weight: 100}})
		require.True(t, picked)
		assert.Equal(t, chHealthy, id)
	}
}

func TestForceOpenUntilAllExcludedFailsOpen(t *testing.T) {
	configureAdaptive(t)
	const chA, chB = 910111, 910112

	ForceOpenUntil(chA, time.Now().Add(time.Minute))
	ForceOpenUntil(chB, time.Now().Add(time.Minute))

	_, picked := Select([]Candidate{{ChannelID: chA, Weight: 100}, {ChannelID: chB, Weight: 100}})
	assert.False(t, picked, "全部冷却时应 fail-open 回退 legacy 选路")
}

func TestForceOpenUntilOnlyExtends(t *testing.T) {
	configureAdaptive(t)
	const ch = 910121

	longUntil := time.Now().Add(time.Hour)
	ForceOpenUntil(ch, longUntil)
	ForceOpenUntil(ch, time.Now().Add(time.Second)) // 更短的冷却不得缩短现有熔断

	s := getStat(ch)
	s.mu.Lock()
	got := s.openUntil
	s.mu.Unlock()
	assert.Equal(t, longUntil, got)
}

func TestForceOpenUntilRespectsDisabledSetting(t *testing.T) {
	setting := configureAdaptive(t)
	setting.Enabled = false
	const ch = 910131

	ForceOpenUntil(ch, time.Now().Add(time.Minute))

	if view, ok := GetStatView(ch); ok {
		assert.False(t, view.CircuitOpen)
	}
}

func TestCurrentInflightCountsWithoutAdaptiveRouting(t *testing.T) {
	setting := configureAdaptive(t)
	setting.Enabled = false // 并发上限依赖在飞计数，必须在自适应路由关闭时也工作
	const ch = 910141

	require.EqualValues(t, 0, CurrentInflight(ch))
	AcquireInflight(ch)
	AcquireInflight(ch)
	assert.EqualValues(t, 2, CurrentInflight(ch))
	ReleaseInflight(ch)
	assert.EqualValues(t, 1, CurrentInflight(ch))
	ReleaseInflight(ch)
	assert.EqualValues(t, 0, CurrentInflight(ch))
}
