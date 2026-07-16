package detector

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIPrefersResponsesSurface(t *testing.T) {
	p := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		path, _ := req["__path__"].(string)
		if strings.Contains(path, "/responses") {
			return responsesOK("gpt-5.6-sol", "I am ChatGPT, developed by OpenAI.", 10, 4)
		}
		return chatOK("gpt-5.6-sol", "chat-surface-text", 10)
	})
	// Relay serves /v1/responses → it becomes the preferred surface.
	p.resolveOpenAISurface(context.Background(), "gpt-5.6-sol")
	assert.Equal(t, surfaceResponses, p.openaiSurface)

	// probeChat now hits /v1/responses and chatContent parses the Responses shape.
	res := p.probeChat(context.Background(), "gpt-5.6-sol", "hi", 64)
	assert.Equal(t, "I am ChatGPT, developed by OpenAI.", p.chatContent(res))
	// Responses probes are unobserved — they must not pollute the chat protocol bus.
	assert.Empty(t, p.tel.snapshot())

	// A shared probe (identity) runs end-to-end over the Responses surface.
	id := chatIdentity(context.Background(), p, cfgFor("gpt-5.6-sol"))
	assert.Equal(t, "pass", id.Status)
}

func TestOpenAIFallsBackToChatWhenNoResponses(t *testing.T) {
	p := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		path, _ := req["__path__"].(string)
		if strings.Contains(path, "/responses") {
			return mockReply{status: 404, body: map[string]interface{}{"error": "no such endpoint"}}
		}
		return chatOK("gpt-4o", "pong", 10)
	})
	p.resolveOpenAISurface(context.Background(), "gpt-4o")
	assert.Equal(t, surfaceChat, p.openaiSurface)
	res := p.probeChat(context.Background(), "gpt-4o", "hi", 64)
	assert.Equal(t, "pong", p.chatContent(res))
}

// responsesOK builds a genuine native Responses envelope (reasoning + message
// items, resp_ id, input/output token usage).
func responsesOK(model, text string, inTok, outTok int) mockReply {
	return mockReply{body: map[string]interface{}{
		"id": "resp_abc123", "object": "response", "status": "completed", "model": model,
		"output": []interface{}{
			map[string]interface{}{"type": "reasoning", "id": "rs_1", "summary": []interface{}{}},
			map[string]interface{}{"type": "message", "id": "msg_1", "status": "completed", "role": "assistant",
				"content": []interface{}{map[string]interface{}{"type": "output_text", "text": text, "annotations": []interface{}{}}}},
		},
		"usage": map[string]interface{}{"input_tokens": inTok, "output_tokens": outTok, "total_tokens": inTok + outTok},
	}}
}

func TestResponsesProtocolNativeE2E(t *testing.T) {
	// Genuine native Responses envelope → pass.
	pGood := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return responsesOK("gpt-5.6-sol", "pong", 8, 2)
	})
	res := responsesProtocol(context.Background(), pGood, cfgFor("gpt-5.6-sol"))
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, 100.0, res.Score)
	assert.Equal(t, true, res.Details["has_reasoning"])

	// Chat-Completions shape on the Responses endpoint → not native → fail.
	pChat := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6-sol", "pong", 8)
	})
	res = responsesProtocol(context.Background(), pChat, cfgFor("gpt-5.6-sol"))
	require.Equal(t, "fail", res.Status)
	issues, _ := res.Details["issues"].([]map[string]interface{})
	assert.NotEmpty(t, issues)

	// Endpoint absent (404) → skip, never a fraud verdict on absence.
	pAbsent := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return mockReply{status: 404, body: map[string]interface{}{"error": map[string]interface{}{"message": "no such endpoint"}}}
	})
	res = responsesProtocol(context.Background(), pAbsent, cfgFor("gpt-5.6-sol"))
	assert.Equal(t, "skip", res.Status)

	// Grok speaks the same Responses format → native envelope passes.
	pGrok := newMockProber(t, ProtocolGrok, func(req map[string]interface{}) mockReply {
		return responsesOK("grok-4.5", "pong", 8, 2)
	})
	res = responsesProtocol(context.Background(), pGrok, cfgFor("grok-4.5"))
	assert.Equal(t, "pass", res.Status)

	// Regression: the Responses probe (object=response / output[]) must NOT be
	// recorded on the passive-observation bus, or the chat-shape openaiProtocol
	// validator would flood it with false criticals. It is unobserved.
	assert.Empty(t, pGood.tel.snapshot(), "Responses probe must not pollute the chat protocol observations")
}

func TestResponsesFunctionCallingE2E(t *testing.T) {
	p := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return mockReply{body: map[string]interface{}{
			"id": "resp_fc", "object": "response", "status": "completed", "model": "gpt-5.6-sol",
			"output": []interface{}{
				map[string]interface{}{"type": "function_call", "id": "fc_1", "call_id": "call_1",
					"name": "get_current_weather", "arguments": `{"city":"Boston","unit":"celsius"}`},
			},
			"usage": map[string]interface{}{"input_tokens": 20, "output_tokens": 8, "total_tokens": 28},
		}}
	})
	res := responsesFunctionCalling(context.Background(), p, cfgFor("gpt-5.6-sol"))
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, 100.0, res.Score)

	// No tool call in the output → fail.
	pNo := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return responsesOK("gpt-5.6-sol", "It is sunny.", 20, 6)
	})
	res = responsesFunctionCalling(context.Background(), pNo, cfgFor("gpt-5.6-sol"))
	assert.Equal(t, "fail", res.Status)
}
