package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/require"
)

// 渠道健康流量上报对所有 relay 格式都必须 nil 安全:RelayInfo 的 Usage 是经
// 嵌入指针 *ClaudeConvertInfo 提升的字段,非 Claude 格式(OpenAI chat/embedding/
// image/audio/gemini 等)不初始化该指针,直接访问 info.Usage 会 panic——这曾让
// 每个非 Claude 格式的成功请求都在 ReportTraffic 求参时崩进 recover。
func TestChannelHealthTrafficNilClaudeConvertInfo(t *testing.T) {
	require.NotPanics(t, func() {
		traffic := channelHealthTraffic(&relaycommon.RelayInfo{}, time.Now())
		require.True(t, traffic.Success)
		require.Zero(t, traffic.OutputTokens)
	})
	require.NotPanics(t, func() {
		_ = channelHealthTraffic(nil, time.Now())
	})
}

// 结算入口写入的 TrafficUsage 必须被流量上报读到(所有文本格式经
// PostTextConsumeQuota 统一接线),且缓存归一口径保持:anthropic 语义下
// 输入补回缓存部分。
func TestChannelHealthTrafficReadsTrafficUsage(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TrafficUsage: &dto.Usage{
			PromptTokens:     100,
			CompletionTokens: 40,
			UsageSemantic:    "anthropic",
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         30,
				CachedCreationTokens: 10,
			},
		},
	}
	traffic := channelHealthTraffic(info, time.Now())
	require.Equal(t, int64(40), traffic.OutputTokens)
	require.Equal(t, int64(140), traffic.InputTokens)
	require.Equal(t, int64(30), traffic.CacheReadTokens)
}
