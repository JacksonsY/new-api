package detector

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeSpeedMetrics(t *testing.T) {
	m := computeSpeedMetrics([]speedRun{
		{ttftMs: 100, totalMs: 1100, outputTokens: 50},
		{ttftMs: 120, totalMs: 1200, outputTokens: 60},
	})
	assert.Equal(t, 2, m.runs)
	assert.Equal(t, int64(110), m.avgTTFTMs)
	assert.Equal(t, int64(1150), m.avgTotalMs)
	assert.Equal(t, 55, m.avgOutTokens)
	// output-phase tps = 110 tokens / ((1000+1080)/1000)s = 52.88
	assert.InDelta(t, 52.88, m.tps, 0.05)
	// total tps = 110 / (2300/1000) = 47.83
	assert.InDelta(t, 47.83, m.totalTPS, 0.05)
	assert.Contains(t, m.summary(), "tok/s")

	// Empty input is safe.
	assert.Equal(t, 0, computeSpeedMetrics(nil).runs)
}

const chatSpeedSSE = `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"An "}}]}

data: {"choices":[{"index":0,"delta":{"content":"API gateway "}}]}

data: {"choices":[{"index":0,"delta":{"content":"routes requests."}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"prompt_tokens":15,"completion_tokens":40,"total_tokens":55}}

data: [DONE]
`

func TestChatSpeedE2E(t *testing.T) {
	p := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return mockReply{rawBody: chatSpeedSSE}
	})
	res := chatSpeed(context.Background(), p, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status)
	require.NotNil(t, res.Details)
	// completion_tokens from the stream usage chunk is used for throughput.
	assert.Equal(t, 40, res.Details["avg_output_tokens"])
	assert.IsType(t, "", res.Details["summary"])
}

const anthropicSpeedSSE = `data: {"type":"message_start","message":{"model":"claude-fable-5","usage":{"input_tokens":15,"output_tokens":1}}}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"An API gateway"}}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" routes requests."}}

data: {"type":"message_delta","usage":{"output_tokens":42}}

data: {"type":"message_stop"}
`

func TestAnthropicSpeedE2E(t *testing.T) {
	p := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return mockReply{rawBody: anthropicSpeedSSE}
	})
	res := anthropicSpeed(context.Background(), p, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status)
	require.NotNil(t, res.Details)
	assert.Equal(t, 42, res.Details["avg_output_tokens"])
	assert.IsType(t, "", res.Details["summary"])
}
