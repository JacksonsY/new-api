package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

// fork 契约：全新部署（未写入任何选项）必须默认具备可用性防护。
// 这些默认值与上游 QuantumNous/new-api 不同（上游全部关闭），
// 合并上游时若被冲回会静默退化为「不重试、不熔断、坏渠道不下线」，
// 本测试用于在合并后第一时间暴露该回归。
func TestResilienceDefaultsEnabled(t *testing.T) {
	assert.Equal(t, 3, common.RetryTimes)
	assert.True(t, common.AutomaticDisableChannelEnabled)
	assert.True(t, common.AutomaticEnableChannelEnabled)

	assert.True(t, monitorSetting.AutoTestChannelEnabled)
	assert.Equal(t, float64(5), monitorSetting.AutoTestChannelMinutes)
	assert.Equal(t, ChannelTestModePassiveRecovery, monitorSetting.ChannelTestMode)

	routing := GetAdaptiveRoutingSetting()
	assert.True(t, routing.Enabled)
	assert.True(t, routing.CircuitEnabled)
	assert.Equal(t, 3, routing.OpenThreshold)
	assert.Equal(t, 30, routing.CooldownSeconds)
	assert.InDelta(t, 0.3, routing.HalfOpenFactor, 1e-9)
	assert.True(t, routing.EscapeEnabled)

	reliability := GetReliabilitySetting()
	assert.True(t, reliability.RateLimitCooldownEnabled)
	assert.Equal(t, 30, reliability.RateLimitCooldownDefaultSeconds)
	assert.Equal(t, 1800, reliability.RateLimitCooldownMaxSeconds)
	assert.True(t, reliability.SameChannelRetryEnabled)
	assert.Equal(t, 1, reliability.SameChannelRetryTimes)
	assert.Equal(t, 300, reliability.SameChannelRetryDelayMs)
}
