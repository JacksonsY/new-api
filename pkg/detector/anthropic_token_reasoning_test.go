package detector

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicAdaptiveThinkingModel(t *testing.T) {
	for _, m := range []string{"claude-opus-4-8", "claude-opus-4-7", "claude-fable-5", "claude-sonnet-5"} {
		assert.True(t, anthropicAdaptiveThinkingModel(m), "%s should be adaptive-thinking", m)
	}
	// Extended-thinking-only and unknown models are not adaptive.
	for _, m := range []string{"claude-haiku-4-5", "claude-opus-4-1", "gpt-5.5", "unknown-model"} {
		assert.False(t, anthropicAdaptiveThinkingModel(m), "%s should not be adaptive-thinking", m)
	}
}

func TestAnthropicStreamInputStable(t *testing.T) {
	in := 11
	ns := map[string]interface{}{"input_tokens": float64(in), "output_tokens": float64(5)}
	streamOut := 17
	stream := anthropicStream{startInput: &in, deltaOutput: &streamOut}
	// Input parity holds and stream output is present → stable, regardless of the
	// large output divergence (5 vs 17).
	assert.True(t, anthropicStreamInputStable(ns, stream))

	// Input mismatch beyond tolerance → not stable.
	badIn := 40
	badStream := anthropicStream{startInput: &badIn, deltaOutput: &streamOut}
	assert.False(t, anthropicStreamInputStable(ns, badStream))
}

func anthropicOKUsage(model, text string, input, output int) mockReply {
	return mockReply{body: map[string]interface{}{
		"id": "msg_01", "type": "message", "role": "assistant", "model": model,
		"content":     []interface{}{map[string]interface{}{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage":       map[string]interface{}{"input_tokens": input, "output_tokens": output},
	}}
}

func anthropicTokenStreamSSE(model string, input, output int) string {
	return "data: {\"type\":\"message_start\",\"message\":{\"model\":\"" + model + "\",\"usage\":{\"input_tokens\":" + strconv.Itoa(input) + ",\"output_tokens\":1}}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":" + strconv.Itoa(output) + "}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
}

// anthropicTokenMock replays short/long/stream/count_tokens for the token_usage
// detector. nsOut is the non-stream output; streamOut is the (divergent) stream
// output — the adaptive-thinking variance that must not false-fail.
func anthropicTokenMock(model string, shortIn, longIn, nsOut, streamOut int) mockUpstreamFn {
	return func(req map[string]interface{}) mockReply {
		if s, _ := req["stream"].(bool); s {
			return mockReply{rawBody: anthropicTokenStreamSSE(model, shortIn, streamOut)}
		}
		if _, hasMax := req["max_tokens"]; !hasMax {
			return mockReply{body: map[string]interface{}{"input_tokens": shortIn}} // count_tokens
		}
		if strings.Contains(msgContent(req, "user"), "Reference text:") {
			return anthropicOKUsage(model, "ok", longIn, nsOut)
		}
		return anthropicOKUsage(model, "ok", shortIn, nsOut)
	}
}

func subCheckPass(details map[string]interface{}, key string) bool {
	sub, _ := details["sub_checks"].(map[string]interface{})
	m, _ := sub[key].(map[string]interface{})
	p, _ := m["pass"].(bool)
	return p
}

func TestAnthropicTokenUsageAdaptiveE2E(t *testing.T) {
	// Adaptive model (opus-4-8): non-stream output 5, stream output 17 — a big
	// per-call divergence from hidden thinking. The output & stream sub-checks must
	// still pass (verify input parity instead of output tolerance).
	pAdaptive := newMockProber(t, ProtocolAnthropic, anthropicTokenMock("claude-opus-4-8", 11, 95, 5, 17))
	res := anthropicTokenUsage(context.Background(), pAdaptive, cfgFor("claude-opus-4-8"))
	require.NotNil(t, res.Details)
	assert.True(t, subCheckPass(res.Details, "output_tokens"), "adaptive output must not be bounded")
	assert.True(t, subCheckPass(res.Details, "stream_usage"), "adaptive stream must verify input parity, not output")

	// Non-adaptive model (haiku-4-5) with the same 5↔17 output divergence: no
	// hidden thinking to explain it, so the stream check still applies its output
	// tolerance and flags it — proving the exemption is model-specific.
	pHaiku := newMockProber(t, ProtocolAnthropic, anthropicTokenMock("claude-haiku-4-5", 11, 95, 5, 17))
	res = anthropicTokenUsage(context.Background(), pHaiku, cfgFor("claude-haiku-4-5"))
	require.NotNil(t, res.Details)
	assert.False(t, subCheckPass(res.Details, "stream_usage"), "non-adaptive output divergence must be flagged")
}
