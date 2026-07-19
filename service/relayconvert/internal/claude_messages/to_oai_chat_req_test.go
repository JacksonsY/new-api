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
// 应透传为 reasoning_effort；但只对 O 系列/GPT-5 设置，且收敛到目标模型接受的取值，
// 否则 gpt-4o 等非推理模型、或 xhigh/max/minimal 会被上游 400。
func TestClaudeMessagesForwardsEffortToOpenAIReasoningModel(t *testing.T) {
	cases := []struct {
		name          string
		upstreamModel string
		claudeEffort  string
		wantEffort    string
	}{
		{"gpt5 high passes", "gpt-5", "high", "high"},
		{"gpt5 xhigh passes (native)", "gpt-5", "xhigh", "xhigh"},
		{"gpt5 max clamps to xhigh", "gpt-5", "max", "xhigh"},
		{"gpt5 minimal kept", "gpt-5", "minimal", "minimal"},
		{"o3 xhigh downgrades to high", "o3-mini", "xhigh", "high"},
		{"o3 minimal downgrades to low", "o3-mini", "minimal", "low"},
		{"o3 high passes", "o3-mini", "high", "high"},
		{"non-reasoning model gets nothing", "gpt-4o", "high", ""},
		{"none maps to nothing", "gpt-5", "none", ""},
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

func TestClampReasoningEffortForOpenAI(t *testing.T) {
	// GPT-5 系列：xhigh 原生透传，max 收敛到 xhigh，minimal 保留
	assert.Equal(t, "high", clampReasoningEffortForOpenAI("high", true))
	assert.Equal(t, "xhigh", clampReasoningEffortForOpenAI("xhigh", true))
	assert.Equal(t, "xhigh", clampReasoningEffortForOpenAI("max", true))
	assert.Equal(t, "minimal", clampReasoningEffortForOpenAI("minimal", true))
	// O 系列：不支持 xhigh/minimal，分别降为 high/low
	assert.Equal(t, "high", clampReasoningEffortForOpenAI("xhigh", false))
	assert.Equal(t, "high", clampReasoningEffortForOpenAI("max", false))
	assert.Equal(t, "low", clampReasoningEffortForOpenAI("minimal", false))
	// 无法映射
	assert.Empty(t, clampReasoningEffortForOpenAI("none", true))
	assert.Empty(t, clampReasoningEffortForOpenAI("", true))
}

func intPtr(v int) *int { return &v }
