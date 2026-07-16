package detector

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// geminiSSE builds a native streamGenerateContent SSE body: text chunks then a
// final chunk carrying finishReason + usageMetadata.
func geminiSSE(texts []string, promptTokens, candTokens int) string {
	var b strings.Builder
	for _, tx := range texts {
		fmt.Fprintf(&b, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":%q}]}}]}\n\n", tx)
	}
	fmt.Fprintf(&b, "data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":%d,\"candidatesTokenCount\":%d,\"totalTokenCount\":%d}}\n\n",
		promptTokens, candTokens, promptTokens+candTokens)
	return b.String()
}

// End-to-end coverage of the NATIVE Gemini battery against a mock
// generateContent upstream: genuine native envelopes pass, and an OpenAI-shape
// response served under the Gemini protocol is caught as swapped-core.

func isGeminiStreamPath(req map[string]interface{}) bool {
	p, _ := req["__path__"].(string)
	return strings.Contains(p, "streamGenerateContent")
}

// geminiNativeUserText returns the user prompt from a native generateContent body.
func geminiNativeUserText(req map[string]interface{}) string {
	contents, _ := req["contents"].([]interface{})
	for _, c := range contents {
		cm, _ := c.(map[string]interface{})
		parts, _ := cm["parts"].([]interface{})
		for _, prt := range parts {
			pm, _ := prt.(map[string]interface{})
			if t, ok := pm["text"].(string); ok {
				return t
			}
		}
	}
	return ""
}

func TestGeminiBasicRequestNativeE2E(t *testing.T) {
	p := newMockProber(t, ProtocolGemini, func(req map[string]interface{}) mockReply {
		return geminiOK("gemini-3.5-flash", "pong", 6)
	})
	res := geminiBasicRequest(context.Background(), p, cfgFor("gemini-3.5-flash"))
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, 100.0, res.Score)
	require.NotNil(t, res.Details)
	assert.Equal(t, "gemini-3.5-flash", res.Details["model_version"])
}

func TestGeminiProtocolNativeE2E(t *testing.T) {
	// Genuine native envelopes → pass.
	clean := geminiProtocol(context.Background(), proberWithObservations(cleanGeminiResponse()), cfgFor("gemini-3.5-flash"))
	assert.Equal(t, "pass", clean.Status)
	assert.Equal(t, 100.0, clean.Score)

	// Swapped core: OpenAI-shape response served as Gemini → critical → fail.
	leak := cleanGeminiResponse()
	leak["choices"] = []interface{}{map[string]interface{}{"index": float64(0)}}
	res := geminiProtocol(context.Background(), proberWithObservations(leak), cfgFor("gemini-3.5-flash"))
	assert.Equal(t, "fail", res.Status)
	assert.GreaterOrEqual(t, res.Details["critical_issue_count"], 1)

	assert.Equal(t, "skip", geminiProtocol(context.Background(), proberWithObservations(), cfgFor("x")).Status)
}

func TestGeminiTokenUsageNativeE2E(t *testing.T) {
	p := newMockProber(t, ProtocolGemini, func(req map[string]interface{}) mockReply {
		// promptTokenCount tracks the user text word count so the short→long delta
		// lands in [45,140] (the long prompt appends 80 " apple" words).
		words := len(strings.Fields(geminiNativeUserText(req)))
		if isGeminiStreamPath(req) {
			sse := geminiSSE([]string{"ok"}, words, 3)
			return mockReply{rawBody: sse}
		}
		return mockReply{body: map[string]interface{}{
			"candidates": []interface{}{map[string]interface{}{
				"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "ok"}}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount": words, "candidatesTokenCount": 3, "totalTokenCount": words + 3,
			},
			"modelVersion": "gemini-3.5-flash",
		}}
	})
	res := geminiTokenUsage(context.Background(), p, cfgFor("gemini-3.5-flash"))
	assert.Equal(t, "pass", res.Status, "native usageMetadata battery should pass a genuine relay")
}

func TestGeminiFunctionCallingNativeE2E(t *testing.T) {
	p := newMockProber(t, ProtocolGemini, func(req map[string]interface{}) mockReply {
		return mockReply{body: map[string]interface{}{
			"candidates": []interface{}{map[string]interface{}{
				"content": map[string]interface{}{"role": "model", "parts": []interface{}{
					map[string]interface{}{"functionCall": map[string]interface{}{
						"name": "get_current_weather",
						"args": map[string]interface{}{"city": "Boston", "unit": "celsius"},
					}},
				}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]interface{}{"promptTokenCount": 20, "candidatesTokenCount": 8, "totalTokenCount": 28},
			"modelVersion":  "gemini-3.5-flash",
		}}
	})
	res := geminiFunctionCalling(context.Background(), p, cfgFor("gemini-3.5-flash"))
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, 100.0, res.Score)
}

func TestGeminiStructuredOutputNativeE2E(t *testing.T) {
	p := newMockProber(t, ProtocolGemini, func(req map[string]interface{}) mockReply {
		return geminiOK("gemini-3.5-flash", `{"ok":true,"nonce":"gemini-detector"}`, 20)
	})
	res := geminiStructuredOutput(context.Background(), p, cfgFor("gemini-3.5-flash"))
	assert.Equal(t, "pass", res.Status)
	require.NotNil(t, res.Details)
	assert.Equal(t, true, res.Details["schema_match"])
}
