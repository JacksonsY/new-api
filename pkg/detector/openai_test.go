package detector

// Phase B 回归：锁定 OpenAI/Gemini 协议对齐——validate_chat_completion 校验器与
// 惩罚模型、gemini _shape_score（不做 usage 指纹扫描）、gemini 专属宽 token 参数
// （slack/大 completion 上限，防误杀 Gemini-3 thinking）、SSE 解析、推理预算耗尽。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cleanChatResponse() map[string]interface{} {
	return map[string]interface{}{
		"id": "chatcmpl-abc123", "object": "chat.completion", "created": float64(1700000000),
		"model": "gpt-5.4-mini",
		"choices": []interface{}{map[string]interface{}{
			"index": float64(0), "finish_reason": "stop",
			"message": map[string]interface{}{"role": "assistant", "content": "ok"},
		}},
		"usage": map[string]interface{}{"prompt_tokens": float64(5), "completion_tokens": float64(3), "total_tokens": float64(8)},
	}
}

func hasCritical(issues []protoIssue) bool {
	for _, i := range issues {
		if i.severity == "critical" {
			return true
		}
	}
	return false
}

func TestValidateChatCompletion(t *testing.T) {
	// Clean official-shaped response → perfect score, no issues. Dotted model
	// matches via normalization (no false model_mismatch major).
	score, issues := validateChatCompletion(cleanChatResponse(), "gpt-5.4-mini")
	assert.Equal(t, 100.0, score)
	assert.Empty(t, issues)

	// Swapped-core: a claude_* usage key is a critical adapter fingerprint.
	swapped := cleanChatResponse()
	swapped["usage"].(map[string]interface{})["claude_cache_creation_5m"] = float64(1)
	score, issues = validateChatCompletion(swapped, "gpt-5.4-mini")
	assert.True(t, hasCritical(issues), "claude_ usage key must be critical")
	assert.Less(t, score, 100.0)

	// A resp_ id (Responses-API era; gpt-5.x returns these even as chat.completion)
	// is a VALID modern OpenAI form, not a critical. Regression guard for the
	// api.jzlh99.com false positive that vetoed a genuine gpt-5.5 relay to marginal.
	respID := cleanChatResponse()
	respID["id"] = "resp_05cec376d156a896016a588defc9cc81"
	score, issues = validateChatCompletion(respID, "gpt-5.4-mini")
	assert.Equal(t, 100.0, score)
	assert.Empty(t, issues)

	// An unrecognized id prefix is a MINOR conformance note, never a verdict-
	// vetoing critical (swapped-core is caught by the usage adapter fingerprints).
	foreignID := cleanChatResponse()
	foreignID["id"] = "weird-xyz-123"
	_, issues = validateChatCompletion(foreignID, "gpt-5.4-mini")
	assert.False(t, hasCritical(issues))
	foundIDMinor := false
	for _, i := range issues {
		if i.code == "id_prefix_unrecognized" {
			assert.Equal(t, "minor", i.severity)
			foundIDMinor = true
		}
	}
	assert.True(t, foundIDMinor, "unrecognized id prefix must surface as a minor note")

	// A wrong object is still critical on its own.
	badObj := cleanChatResponse()
	badObj["object"] = "text_completion"
	_, issues = validateChatCompletion(badObj, "gpt-5.4-mini")
	assert.True(t, hasCritical(issues))

	// Model mismatch is a major (not critical).
	mism := cleanChatResponse()
	mism["model"] = "claude-3-opus"
	_, issues = validateChatCompletion(mism, "gpt-5.4-mini")
	assert.False(t, hasCritical(issues))
	found := false
	for _, i := range issues {
		if i.code == "model_mismatch" {
			found = true
		}
	}
	assert.True(t, found)
}

// cleanGeminiResponse is a well-formed NATIVE generateContent envelope.
func cleanGeminiResponse() map[string]interface{} {
	return map[string]interface{}{
		"candidates": []interface{}{map[string]interface{}{
			"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "ok"}}},
			"finishReason": "STOP", "index": float64(0),
		}},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount": float64(5), "candidatesTokenCount": float64(3), "totalTokenCount": float64(8),
		},
		"modelVersion": "gemini-2.5-flash",
		"responseId":   "abc-123",
	}
}

func TestGeminiNativeShapeScore(t *testing.T) {
	// Clean native envelope → 100, no issues.
	score, issues := geminiNativeShapeScore(cleanGeminiResponse())
	assert.Equal(t, 100.0, score)
	assert.Empty(t, issues)

	// Swapped-core leak: an OpenAI-envelope field (choices) on a "gemini"
	// response is a critical substitution tell.
	leak := cleanGeminiResponse()
	leak["choices"] = []interface{}{}
	_, issues = geminiNativeShapeScore(leak)
	assert.True(t, hasCritical(issues))

	// object=chat.completion is the compat-layer-impersonating-native tell.
	obj := cleanGeminiResponse()
	obj["object"] = "chat.completion"
	_, issues = geminiNativeShapeScore(obj)
	assert.True(t, hasCritical(issues))

	// Missing candidates → critical + lower score.
	noCand := cleanGeminiResponse()
	delete(noCand, "candidates")
	score, issues = geminiNativeShapeScore(noCand)
	assert.True(t, hasCritical(issues))
	assert.Less(t, score, 100.0)
}

func TestOpenAIProtocolPassive(t *testing.T) {
	clean := openaiProtocol(context.Background(), proberWithObservations(cleanChatResponse()), Config{Model: "gpt-5.4-mini"})
	assert.Equal(t, "pass", clean.Status)
	assert.Equal(t, 100.0, clean.Score)

	swapped := cleanChatResponse()
	swapped["usage"].(map[string]interface{})["gemini_thoughts"] = float64(1)
	res := openaiProtocol(context.Background(), proberWithObservations(swapped), Config{Model: "gpt-5.4-mini"})
	assert.Equal(t, "fail", res.Status) // any critical → fail regardless of avg
	assert.GreaterOrEqual(t, res.Details["critical_issue_count"], 1)

	assert.Equal(t, "skip", openaiProtocol(context.Background(), proberWithObservations(), Config{}).Status)
}

func TestOpenAIParseStream(t *testing.T) {
	objs := []map[string]interface{}{
		{"model": "gpt-x", "choices": []interface{}{map[string]interface{}{"delta": map[string]interface{}{"content": "he"}}}},
		{"choices": []interface{}{map[string]interface{}{"delta": map[string]interface{}{"content": "llo"}, "finish_reason": "stop"}}},
		{"usage": map[string]interface{}{"prompt_tokens": float64(5), "completion_tokens": float64(2), "total_tokens": float64(7)}},
	}
	s := openaiParseStream(objs)
	assert.Equal(t, "hello", s.text)
	assert.Equal(t, "stop", s.finishReason)
	require.NotNil(t, s.usage)
	v, _ := intField(s.usage, "prompt_tokens")
	assert.Equal(t, 5, v)
}

func TestOpenAIReasoningExhausted(t *testing.T) {
	u := map[string]interface{}{
		"completion_tokens":         float64(10),
		"completion_tokens_details": map[string]interface{}{"reasoning_tokens": float64(10)},
	}
	assert.True(t, openaiReasoningExhausted(u, "length", ""))    // empty + length + reasoning ate budget
	assert.False(t, openaiReasoningExhausted(u, "length", "hi")) // has text → not exhausted
	assert.False(t, openaiReasoningExhausted(u, "stop", ""))     // finished normally
}

func TestGeminiTokenBudgets(t *testing.T) {
	// Native arithmetic: totalTokenCount ≈ prompt + candidates + thoughts within
	// 5. The thinking tokens are part of the total, so a thinking model still
	// balances.
	assert.True(t, geminiArithmeticOK(map[string]interface{}{
		"promptTokenCount": float64(5), "candidatesTokenCount": float64(3),
		"thoughtsTokenCount": float64(40), "totalTokenCount": float64(48),
	}))
	assert.False(t, geminiArithmeticOK(map[string]interface{}{
		"promptTokenCount": float64(5), "candidatesTokenCount": float64(3), "totalTokenCount": float64(20),
	}))

	// Visible-output cap is MAX+5=133; thoughtsTokenCount is separate and
	// unbounded, so candidatesTokenCount stays within the requested max.
	assert.True(t, geminiCandidatesSane(map[string]interface{}{"candidatesTokenCount": float64(128)}))
	assert.True(t, geminiCandidatesSane(map[string]interface{}{"candidatesTokenCount": float64(133)}))
	assert.False(t, geminiCandidatesSane(map[string]interface{}{"candidatesTokenCount": float64(134)}))
}

func TestNormalizeChatTextAndUsageClose(t *testing.T) {
	assert.Equal(t, "veridrop stream check", normalizeChatText("  Veridrop   Stream Check. "))
	assert.True(t, usageCloseAll(
		map[string]interface{}{"prompt_tokens": float64(10), "completion_tokens": float64(4), "total_tokens": float64(14)},
		map[string]interface{}{"prompt_tokens": float64(10), "completion_tokens": float64(5), "total_tokens": float64(15)},
		2))
	assert.False(t, usageCloseAll(
		map[string]interface{}{"prompt_tokens": float64(10)},
		map[string]interface{}{"prompt_tokens": float64(99)},
		2))
}
