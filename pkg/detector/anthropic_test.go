package detector

// Phase A 回归：锁定对齐 Veridrop anthropic 协议的关键行为——温度净化（防误伤
// 真 opus-4-7/4-8）、模型能力表、CV/相似度、SSE 解析、被动 protocol/message_id
// 打分。全部纯函数或合成观测总线，无网络。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicOmitsTemperature(t *testing.T) {
	// opus-4-7 / opus-4-8（含快照/点号形式）必须省略 temperature，真模型对它 400。
	for _, m := range []string{"claude-opus-4-8", "claude-opus-4-8-20260101", "claude-opus-4.8", "claude-opus-4-7"} {
		assert.True(t, anthropicOmitsTemperature(m), m)
		_, hasTemp := anthropicPayload(m, "hi", 16)["temperature"]
		assert.False(t, hasTemp, "payload for %s must not carry temperature", m)
	}
	// 其余模型保留 temperature=0 以求确定性。
	for _, m := range []string{"claude-sonnet-4-6", "claude-haiku-4-5", "claude-opus-4-6", "gpt-4o"} {
		assert.False(t, anthropicOmitsTemperature(m), m)
		_, hasTemp := anthropicPayload(m, "hi", 16)["temperature"]
		assert.True(t, hasTemp, "payload for %s must carry temperature", m)
	}
}

func TestAnthropicModelTable(t *testing.T) {
	// 快照后缀可匹配，点号可匹配。
	require.NotNil(t, lookupModel("claude-opus-4-8-20260101"))
	require.NotNil(t, lookupModel("claude-sonnet-4.6"))
	assert.Nil(t, lookupModel("gpt-4o"))

	// applies_to：已知会思考的模型 → true；未知模型 → false（皇冠检测跳过而非误判）。
	assert.True(t, modelSupportsThinking("claude-opus-4-8"))
	assert.True(t, modelSupportsThinking("claude-haiku-4-5"))
	assert.False(t, modelSupportsThinking("gpt-4o"))

	// adaptive effort：opus-4-7/4-8 → xhigh，其余 → high。
	assert.Equal(t, "xhigh", adaptiveEffortForModel("claude-opus-4-7"))
	assert.Equal(t, "xhigh", adaptiveEffortForModel("claude-opus-4-8-20260101"))
	assert.Equal(t, "high", adaptiveEffortForModel("claude-haiku-4-5"))

	// 新分词器 delta 区间：opus-4-8 用宽区间，haiku 用窄区间。
	assert.True(t, modelUsesNewTokenizer("claude-opus-4-8"))
	assert.False(t, modelUsesNewTokenizer("claude-haiku-4-5"))
	dmin, dmax := anthropicDeltaRange("claude-opus-4-8")
	assert.Equal(t, [2]int{90, 230}, [2]int{dmin, dmax})
	dmin, dmax = anthropicDeltaRange("claude-haiku-4-5")
	assert.Equal(t, [2]int{45, 140}, [2]int{dmin, dmax})
}

func TestStringRatio(t *testing.T) {
	assert.Equal(t, 100.0, stringRatio("hello world", "hello world"))
	assert.Equal(t, 100.0, stringRatio("", ""))
	assert.Equal(t, 0.0, stringRatio("abc", ""))
	assert.Equal(t, 0.0, stringRatio("abc", "xyz")) // no common subsequence
	// near-identical stays above the integrity 85 threshold; disjoint does not.
	assert.Greater(t, stringRatio(`{"verify":"abc123","n":42}`, `{"verify":"abc123","n":42} `), 85.0)
	assert.Less(t, stringRatio("the quick brown fox", "zzzzzzz"), 85.0)
}

func TestParseAnthropicStream(t *testing.T) {
	objs := []map[string]interface{}{
		{"type": "message_start", "message": map[string]interface{}{
			"model": "claude-opus-4-8", "usage": map[string]interface{}{"input_tokens": float64(10)},
		}},
		{"type": "content_block_delta", "delta": map[string]interface{}{"type": "text_delta", "text": "he"}},
		{"type": "content_block_delta", "delta": map[string]interface{}{"type": "text_delta", "text": "llo"}},
		{"type": "message_delta", "delta": map[string]interface{}{"stop_reason": "end_turn"},
			"usage": map[string]interface{}{"output_tokens": float64(4)}},
	}
	s := parseAnthropicStream(objs)
	assert.Equal(t, "claude-opus-4-8", s.model)
	assert.Equal(t, "hello", s.text)
	assert.Equal(t, "end_turn", s.stopReason)
	require.NotNil(t, s.startInput)
	assert.Equal(t, 10, *s.startInput)
	require.NotNil(t, s.deltaOutput)
	assert.Equal(t, 4, *s.deltaOutput)
}

// proberWithObservations builds a prober whose bus is pre-seeded, so the passive
// detectors can be exercised deterministically without any network.
func proberWithObservations(resps ...map[string]interface{}) *prober {
	tel := &runTelemetry{}
	for _, r := range resps {
		tel.observations = append(tel.observations, observation{response: r, statusCode: 200})
	}
	return &prober{tel: tel}
}

func cleanAnthropicResponse() map[string]interface{} {
	return map[string]interface{}{
		"id": "msg_0123456789", "type": "message", "role": "assistant",
		"model":       "claude-opus-4-8",
		"content":     []interface{}{map[string]interface{}{"type": "text", "text": "hi"}},
		"stop_reason": "end_turn",
		"usage":       map[string]interface{}{"input_tokens": float64(5), "output_tokens": float64(3)},
	}
}

func TestAnthropicProtocolPassive(t *testing.T) {
	// A clean Messages response → no issues → score 100.
	clean := anthropicProtocol(context.Background(), proberWithObservations(cleanAnthropicResponse()), Config{})
	assert.Equal(t, "pass", clean.Status)
	assert.Equal(t, 100.0, clean.Score)

	// No observations → skip (never fabricate a score).
	skip := anthropicProtocol(context.Background(), proberWithObservations(), Config{})
	assert.Equal(t, "skip", skip.Status)

	// A malformed response accrues distinct −10 issues (id/type/role/model/content
	// + usage arithmetic), so the score drops well below pass.
	broken := map[string]interface{}{
		"id": "", "type": "chat", "role": "user", "model": "",
		"content": "not-an-array",
		"usage":   map[string]interface{}{"input_tokens": float64(-1)},
	}
	res := anthropicProtocol(context.Background(), proberWithObservations(broken), Config{})
	assert.Equal(t, "fail", res.Status)
	assert.Less(t, res.Score, 70.0)
	assert.GreaterOrEqual(t, res.Details["issue_count"], 5)
}

func TestAnthropicMessageIDPassive(t *testing.T) {
	ok := anthropicMessageID(context.Background(), proberWithObservations(cleanAnthropicResponse()), Config{})
	assert.Equal(t, "pass", ok.Status)
	assert.Equal(t, 100.0, ok.Score)

	// id/type/role/model all wrong → 4 base violations × −25 → 0.
	bad := map[string]interface{}{
		"id": "x", "type": "chat", "role": "user", "model": "gpt-4o",
		"content": []interface{}{},
	}
	res := anthropicMessageID(context.Background(), proberWithObservations(bad), Config{})
	assert.Equal(t, "fail", res.Status)
	assert.Equal(t, 0.0, res.Score)
}
