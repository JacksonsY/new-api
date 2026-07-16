package detector

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenAIReasoningModel(t *testing.T) {
	for _, m := range []string{"gpt-5.5", "gpt-5.6", "gpt-5.4-mini", "o1-preview", "o3-mini", "o4"} {
		assert.True(t, openaiReasoningModel(m), "%s should be a reasoning model", m)
	}
	for _, m := range []string{"gpt-4o", "gpt-4.1", "claude-fable-5", "gemini-3-pro", ""} {
		assert.False(t, openaiReasoningModel(m), "%s should not be a reasoning model", m)
	}
}

func TestReasoningBrokenOut(t *testing.T) {
	withDetails := map[string]interface{}{
		"completion_tokens": float64(17),
		"completion_tokens_details": map[string]interface{}{
			"reasoning_tokens": float64(15),
		},
	}
	assert.True(t, reasoningBrokenOut(withDetails))
	assert.False(t, reasoningBrokenOut(map[string]interface{}{"completion_tokens": float64(17)}))
	assert.False(t, reasoningBrokenOut(nil))
}

// chatOKUsage is chatOK with an explicit usage triple (no reasoning breakdown).
func chatOKUsage(model, text string, prompt, completion int) mockReply {
	return mockReply{body: map[string]interface{}{
		"id": "resp_abc", "object": "chat.completion", "model": model,
		"choices": []interface{}{map[string]interface{}{
			"index": 0, "message": map[string]interface{}{"role": "assistant", "content": text},
			"finish_reason": "stop",
		}},
		"usage": map[string]interface{}{
			"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion,
		},
	}}
}

func openaiUsageChunkSSE(prompt, completion int) string {
	return "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":" +
		strconv.Itoa(prompt) + ",\"completion_tokens\":" + strconv.Itoa(completion) + ",\"total_tokens\":" + strconv.Itoa(prompt+completion) + "}}\n\n" +
		"data: [DONE]\n\n"
}

// tokenBillingMock replays the short/long/stream responses the token-billing
// detector expects, with the given (prompt, completion) usage per leg. Completion
// is inflated for a short "ok" answer with NO reasoning_tokens breakdown — the
// exact api.jzlh99.com gpt-5.5 shape.
func tokenBillingMock(model string) mockUpstreamFn {
	return func(req map[string]interface{}) mockReply {
		if s, _ := req["stream"].(bool); s {
			return mockReply{rawBody: openaiUsageChunkSSE(11, 17)}
		}
		if strings.Contains(msgContent(req, "user"), "Reference text:") {
			return chatOKUsage(model, "ok", 95, 17) // long prompt, inflated completion
		}
		return chatOKUsage(model, "ok", 11, 5) // short prompt
	}
}

func TestOpenAITokenBillingReasoningE2E(t *testing.T) {
	// gpt-5.5 (reasoning) with inflated completion and NO reasoning_tokens
	// breakdown must NOT false-fail token billing — regression for api.jzlh99.com.
	pReasoning := newMockProber(t, ProtocolOpenAI, tokenBillingMock("gpt-5.5"))
	res := openaiTokenBilling(context.Background(), pReasoning, cfgFor("gpt-5.5"))
	assert.Equal(t, "pass", res.Status)
	assert.GreaterOrEqual(t, res.Score, 90.0)

	// A NON-reasoning model (gpt-4o) with the same "ok"→17 completion IS anomalous
	// (no hidden reasoning to explain it), so the calibration must still flag it —
	// proving the relaxation is model-specific, not a blanket weakening.
	pPlain := newMockProber(t, ProtocolOpenAI, tokenBillingMock("gpt-4o"))
	res = openaiTokenBilling(context.Background(), pPlain, cfgFor("gpt-4o"))
	assert.Equal(t, "fail", res.Status)
	assert.Less(t, res.Score, 90.0)
}
