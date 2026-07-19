package claudemessages

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// issue #5922：Claude 客户端的 effort(output_config.effort) 到 OpenAI 推理模型
// 原样透传为 reasoning_effort；仅对 O 系列/GPT-5 设置(gpt-4o 等非推理模型带 effort
// 会被上游 400)。不按模型硬编码降级——各模型支持面模型相关、难可靠核实，硬降级会
// 静默篡改客户端明确选择;不支持某档由目标模型自身返回 400。
func TestClaudeMessagesForwardsEffortToOpenAIReasoningModel(t *testing.T) {
	cases := []struct {
		name          string
		upstreamModel string
		claudeEffort  string
		wantEffort    string
	}{
		{"gpt5 high passes", "gpt-5", "high", "high"},
		{"gpt5 xhigh passes through", "gpt-5", "xhigh", "xhigh"},
		{"gpt5 max passes through", "gpt-5", "max", "max"},
		{"gpt5 minimal passes through", "gpt-5", "minimal", "minimal"},
		{"o3 xhigh passes through", "o3-mini", "xhigh", "xhigh"},
		{"o3 minimal passes through", "o3-mini", "minimal", "minimal"},
		{"o3 high passes", "o3-mini", "high", "high"},
		{"non-reasoning model gets nothing", "gpt-4o", "high", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claudeRequest := dto.ClaudeRequest{
				Model:        tc.upstreamModel,
				OutputConfig: json.RawMessage(`{"effort":"` + tc.claudeEffort + `"}`),
			}
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeOpenAI,
					UpstreamModelName: tc.upstreamModel,
				},
			}
			openAIRequest, err := ClaudeMessagesRequestToOpenAIChat(claudeRequest, info)
			require.NoError(t, err)
			assert.Equal(t, tc.wantEffort, openAIRequest.ReasoningEffort)
		})
	}
}

// OpenRouter 渠道走独立的 reasoning 字段，不设 reasoning_effort（保持原行为）。
func TestClaudeMessagesOpenRouterUsesReasoningNotEffort(t *testing.T) {
	claudeRequest := dto.ClaudeRequest{
		Model:        "anthropic/claude-sonnet-5",
		OutputConfig: json.RawMessage(`{"effort":"high"}`),
		Thinking:     &dto.Thinking{Type: "enabled", BudgetTokens: intPtr(2048)},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter},
	}

	openAIRequest, err := ClaudeMessagesRequestToOpenAIChat(claudeRequest, info)
	require.NoError(t, err)
	assert.Empty(t, openAIRequest.ReasoningEffort)
	assert.NotEmpty(t, openAIRequest.Reasoning)
}

// info.ChannelMeta 为 nil 时不得 nil 解引用(UpstreamModelName 由 *ChannelMeta 提升)。
func TestClaudeMessagesEffortForwardingNilChannelMetaSafe(t *testing.T) {
	claudeRequest := dto.ClaudeRequest{
		Model:        "gpt-5",
		OutputConfig: json.RawMessage(`{"effort":"high"}`),
	}
	info := &relaycommon.RelayInfo{} // ChannelMeta 为 nil
	assert.NotPanics(t, func() {
		openAIRequest, err := ClaudeMessagesRequestToOpenAIChat(claudeRequest, info)
		require.NoError(t, err)
		assert.Empty(t, openAIRequest.ReasoningEffort)
	})
}

func intPtr(v int) *int { return &v }
