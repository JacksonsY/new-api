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

	// Wrong id prefix + wrong object are each critical.
	bad := cleanChatResponse()
	bad["id"] = "resp_xyz"
	bad["object"] = "text_completion"
	_, issues = validateChatCompletion(bad, "gpt-5.4-mini")
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

func TestGeminiShapeScoreNoFingerprintScan(t *testing.T) {
	// Clean → 100.
	score, issues := geminiShapeScore(cleanChatResponse())
	assert.Equal(t, 100.0, score)
	assert.Empty(t, issues)

	// A claude_* usage key must NOT make Gemini critical — gemini protocol has
	// no adapter-fingerprint scan (the Go reuse of openaiProtocol wrongly added
	// one; this locks the correct gemini behavior).
	withFP := cleanChatResponse()
	withFP["usage"].(map[string]interface{})["claude_cache_creation_5m"] = float64(1)
	score, issues = geminiShapeScore(withFP)
	assert.Equal(t, 100.0, score)
	assert.False(t, hasCritical(issues))

	// Missing id → critical + bad prefix.
	noID := cleanChatResponse()
	delete(noID, "id")
	score, issues = geminiShapeScore(noID)
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
	// Arithmetic allows ≤5 slack (OpenAI billing requires exact) — Gemini
	// reasoning summaries add overhead tokens.
	assert.True(t, geminiArithmeticOK(map[string]interface{}{"prompt_tokens": float64(5), "completion_tokens": float64(3), "total_tokens": float64(9)}))
	assert.False(t, geminiArithmeticOK(map[string]interface{}{"prompt_tokens": float64(5), "completion_tokens": float64(3), "total_tokens": float64(20)}))

	// Completion cap is MAX+5=133 (OpenAI billing caps at 12) — thinking models
	// fill the whole budget with reasoning tokens on a "say ok" prompt.
	assert.True(t, geminiCompletionSane(map[string]interface{}{"completion_tokens": float64(128)}))
	assert.True(t, geminiCompletionSane(map[string]interface{}{"completion_tokens": float64(133)}))
	assert.False(t, geminiCompletionSane(map[string]interface{}{"completion_tokens": float64(134)}))
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
