package detector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// cleanStreamObjs builds a well-formed Anthropic SSE object sequence. Numbers are
// float64 to match JSON-decoded streams (what intField expects).
func cleanStreamObjs() []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "message_start", "message": map[string]interface{}{
			"model": "claude-fable-5", "usage": map[string]interface{}{"input_tokens": float64(10)},
		}},
		{"type": "content_block_start", "content_block": map[string]interface{}{"type": "text"}},
		{"type": "content_block_delta", "delta": map[string]interface{}{"type": "text_delta", "text": "red"}},
		{"type": "content_block_stop"},
		{"type": "message_delta", "usage": map[string]interface{}{"output_tokens": float64(5)}},
		{"type": "message_stop"},
	}
}

func TestAnalyzeAnthropicStream(t *testing.T) {
	// Clean, well-formed Anthropic stream.
	v := analyzeAnthropicStream(collectAnthropicStreamSignals(cleanStreamObjs()))
	assert.Equal(t, "clean", v.verdict)
	assert.False(t, v.modelSubstituted)

	// Unknown SSE event type → anomaly.
	objs := append(cleanStreamObjs(), map[string]interface{}{"type": "inject_event"})
	v = analyzeAnthropicStream(collectAnthropicStreamSignals(objs))
	assert.Equal(t, "anomaly", v.verdict)
	assert.Contains(t, v.unknownEvents, "inject_event")

	// Non-Claude message_start model → substitution anomaly.
	objs = cleanStreamObjs()
	objs[0]["message"].(map[string]interface{})["model"] = "gpt-4o"
	v = analyzeAnthropicStream(collectAnthropicStreamSignals(objs))
	assert.Equal(t, "anomaly", v.verdict)
	assert.True(t, v.modelSubstituted)

	// Non-monotonic output_tokens across message_delta → usage rewrite anomaly.
	objs = cleanStreamObjs()
	objs = append(objs, map[string]interface{}{"type": "message_delta", "usage": map[string]interface{}{"output_tokens": float64(3)}})
	v = analyzeAnthropicStream(collectAnthropicStreamSignals(objs))
	assert.Equal(t, "anomaly", v.verdict)

	// Empty thinking signature_delta → downgrade anomaly.
	objs = cleanStreamObjs()
	objs = append(objs, map[string]interface{}{"type": "content_block_delta", "delta": map[string]interface{}{"type": "signature_delta", "signature": ""}})
	v = analyzeAnthropicStream(collectAnthropicStreamSignals(objs))
	assert.Equal(t, "anomaly", v.verdict)

	// No substantive events (only ping) → inconclusive.
	v = analyzeAnthropicStream(collectAnthropicStreamSignals([]map[string]interface{}{{"type": "ping"}}))
	assert.Equal(t, "inconclusive", v.verdict)
	// Empty stream → inconclusive.
	v = analyzeAnthropicStream(collectAnthropicStreamSignals(nil))
	assert.Equal(t, "inconclusive", v.verdict)
}
