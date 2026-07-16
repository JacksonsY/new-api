package detector

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Grok (xAI) support: protocol inference/normalization, reasoning-billing
// exemptions, UUID response-id acceptance, context-limit resolution, and the
// chat identity detector's vendor-anchored verdicts.

func TestGrokProtocolRouting(t *testing.T) {
	// Model-name inference and explicit protocol both preserve "grok" in the
	// report while the openai battery serves it (selectDetectors default).
	assert.Equal(t, ProtocolGrok, inferProtocol("grok-4.5"))
	assert.Equal(t, ProtocolOpenAI, inferProtocol("gpt-5.6-sol"))

	cfg := normalizeConfig(Config{Model: "grok-4.5", Protocol: "grok"})
	assert.Equal(t, ProtocolGrok, cfg.Protocol)
	cfg = normalizeConfig(Config{Model: "grok-4.3"})
	assert.Equal(t, ProtocolGrok, cfg.Protocol)

	names := make(map[string]bool)
	for _, d := range selectDetectors(normalizeConfig(Config{Model: "grok-4.5", Protocol: "grok", Mode: ModeFull})) {
		names[d.name] = true
	}
	assert.True(t, names[detectorIdentity], "grok runs the chat identity detector")
	assert.True(t, names[detectorTokenBilling], "grok shares the openai battery")
}

func TestGrokReasoningBillingExemption(t *testing.T) {
	for _, m := range []string{"grok-4.5", "grok-4.3", "grok-4.20-0309-reasoning", "grok-4.20-multi-agent-0309", "grok-4.1-fast-reasoning", "grok-code-fast-1", "grok-build-0.1", "grok-3-mini"} {
		assert.True(t, openaiReasoningModel(m), m)
	}
	for _, m := range []string{"grok-4.20-0309-non-reasoning", "grok-4.1-fast-non-reasoning", "grok-3", "grok-3-fast", "grok-2-1212", "gpt-4o"} {
		assert.False(t, openaiReasoningModel(m), m)
	}
}

func TestGrokUUIDResponseID(t *testing.T) {
	payload := map[string]interface{}{
		"id": "0daf962f-a275-4a3c-839a-047854645532", "object": "chat.completion",
		"created": 1, "model": "grok-4.5",
		"choices": []interface{}{map[string]interface{}{
			"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "ok"},
			"finish_reason": "stop",
		}},
		"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}
	hasIDIssue := func(model string) bool {
		_, issues := validateChatCompletion(payload, model)
		for _, is := range issues {
			if is.code == "id_prefix_unrecognized" {
				return true
			}
		}
		return false
	}
	// The UUID form is xAI's genuine id shape — clean for a grok target, still a
	// minor conformance note for a gpt target.
	assert.False(t, hasIDIssue("grok-4.5"))
	payload["model"] = "gpt-5.6-sol"
	assert.True(t, hasIDIssue("gpt-5.6-sol"))
}

func TestGrokContextLimits(t *testing.T) {
	assert.Equal(t, 500_000, modelContextLimit("grok-4.5"))
	assert.Equal(t, 1_000_000, modelContextLimit("grok-4.3"))
	assert.Equal(t, 1_000_000, modelContextLimit("grok-4.20-0309-reasoning"))
	assert.Equal(t, 2_000_000, modelContextLimit("grok-4.1-fast-reasoning"))
	assert.Equal(t, 256_000, modelContextLimit("grok-4-0709"))
	assert.Equal(t, 256_000, modelContextLimit("grok-build-0.1"))
	assert.Equal(t, 131_072, modelContextLimit("grok-3-mini"))
}

func TestChatIdentityGrokE2E(t *testing.T) {
	// Genuine grok self-id → full pass.
	pGood := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("grok-4.5", "I am Grok, an AI model built by xAI.", 20)
	})
	res := chatIdentity(context.Background(), pGood, cfgFor("grok-4.5"))
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, 100.0, res.Score)

	// Grok target answering as ChatGPT/OpenAI → swapped core.
	pSwap := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("grok-4.5", "I am ChatGPT, a language model developed by OpenAI.", 20)
	})
	res = chatIdentity(context.Background(), pSwap, cfgFor("grok-4.5"))
	require.Equal(t, "fail", res.Status)
	assert.Equal(t, 0.0, res.Score)
	brands, ok := res.Details["detected_foreign_brands"].([]string)
	require.True(t, ok)
	assert.Contains(t, brands, "OpenAI")

	// Reverse direction: gpt target answering as Grok → same evidence class.
	pRev := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("gpt-5.6-sol", "I'm Grok, created by xAI.", 20)
	})
	res = chatIdentity(context.Background(), pRev, cfgFor("gpt-5.6-sol"))
	require.Equal(t, "fail", res.Status)
	brands, _ = res.Details["detected_foreign_brands"].([]string)
	assert.Contains(t, brands, "Grok")

	// Brand-free answer (relay-injected system prompt) → pass-with-note, never
	// a fraud verdict on absence of evidence.
	pMute := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("grok-4.5", "I'm a large language model here to help you.", 20)
	})
	res = chatIdentity(context.Background(), pMute, cfgFor("grok-4.5"))
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, 60.0, res.Score)

	// Unknown vendor (open-weights): identity recorded, not judged.
	pOpen := newMockProber(t, ProtocolOpenAI, func(req map[string]interface{}) mockReply {
		return chatOK("llama-3.3-70b", "I am LLaMA, developed by Meta.", 20)
	})
	res = chatIdentity(context.Background(), pOpen, cfgFor("llama-3.3-70b"))
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, 100.0, res.Score)
}

func TestChatIdentityGeminiOwnBrand(t *testing.T) {
	// "trained by Google" without the word Gemini is still a full own-brand hit
	// (any-own-marker calibration — deviation from the anthropic 4-tier).
	p := newMockProber(t, ProtocolGemini, func(req map[string]interface{}) mockReply {
		return geminiOK("gemini-3.5-flash", "I am a large language model, trained by Google.", 20)
	})
	res := chatIdentity(context.Background(), p, cfgFor("gemini-3.5-flash"))
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, 100.0, res.Score)
}
