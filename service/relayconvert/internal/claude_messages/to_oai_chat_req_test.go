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

// issue #5922：Claude 客户端的显式 effort(output_config.effort) 到纯 OpenAI 兼容
// 渠道应透传为 reasoning_effort;此前只在 OpenRouter 分支处理、纯 OpenAI 渠道丢失。
func TestClaudeMessagesForwardsEffortToOpenAIChannel(t *testing.T) {
	claudeRequest := dto.ClaudeRequest{
		Model:        "gpt-5",
		OutputConfig: json.RawMessage(`{"effort":"high"}`),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
	}

	openAIRequest, err := ClaudeMessagesRequestToOpenAIChat(claudeRequest, info)
	require.NoError(t, err)
	assert.Equal(t, "high", openAIRequest.ReasoningEffort)
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

func intPtr(v int) *int { return &v }
