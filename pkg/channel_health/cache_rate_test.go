package channelhealth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 缓存命中率观测契约：ReportTraffic 记录总输入/缓存读取 tokens 的
// 60s 窗口与自启动累计，经 StatView 暴露给渠道健康面板。
func TestReportTrafficCacheTokens(t *testing.T) {
	configureAdaptive(t)
	const ch = 940001

	ReportTraffic(ch, Traffic{Success: true, LatencyMs: 100, InputTokens: 1000, CacheReadTokens: 800})
	ReportTraffic(ch, Traffic{Success: true, LatencyMs: 100, InputTokens: 500, CacheReadTokens: 0})

	view, ok := GetStatView(ch)
	require.True(t, ok)
	assert.EqualValues(t, 1500, view.InputTpm)
	assert.EqualValues(t, 800, view.CacheTpm)
	assert.EqualValues(t, 1500, view.InputTokensTotal)
	assert.EqualValues(t, 800, view.CacheReadTokensTotal)

	// 失败请求不产生输入统计（Success=false 分支只记故障分类）
	ReportTraffic(ch, Traffic{Success: false, ChannelFault: true, ErrCode: 500, InputTokens: 999})
	view, _ = GetStatView(ch)
	assert.EqualValues(t, 1500, view.InputTokensTotal)
}
