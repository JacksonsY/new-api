package detector

// Phase D 回归：baseline + comparator——嵌入基线可解析、自比一致(无 critical)、
// 相对真值的 critical（新后端品牌 / thinking 签名丢失 / 模型不匹配）。这些是单轮
// 绝对检测发不出的"相对基线"杀手锏。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cloneReport deep-copies a report's results + detail maps so a test can mutate
// the relay side without corrupting the shared baseline cache.
func cloneReport(r *Report) *Report {
	cp := *r
	cp.Results = make([]DetectorResult, len(r.Results))
	for i, res := range r.Results {
		cp.Results[i] = res
		nd := map[string]interface{}{}
		for k, v := range res.Details {
			nd[k] = v
		}
		cp.Results[i].Details = nd
	}
	return &cp
}

func findResult(rep *Report, name string) *DetectorResult {
	for i := range rep.Results {
		if rep.Results[i].Name == name {
			return &rep.Results[i]
		}
	}
	return nil
}

func TestFindBaseline(t *testing.T) {
	require.NotNil(t, findBaseline("claude-haiku-4-5", "full"), "haiku full baseline must be embedded + parseable")
	require.NotNil(t, findBaseline("claude-haiku-4-5-20251001", "full"), "snapshot suffix must match")
	assert.Nil(t, findBaseline("gpt-4o", "full"), "no baseline for non-Claude")
	assert.Nil(t, findBaseline("claude-haiku-4-5", "quick"), "baselines are full-mode only")

	b := findBaseline("claude-haiku-4-5", "full")
	assert.Equal(t, "claude-haiku-4-5", b.TargetModel)
	assert.NotEmpty(t, b.Results, "baseline must carry detector results")
}

func TestCompareIdenticalIsClean(t *testing.T) {
	baseline := findBaseline("claude-haiku-4-5", "full")
	require.NotNil(t, baseline)
	cmp := compareToBaseline(baseline, cloneReport(baseline))
	assert.Equal(t, sevOK, cmp.OverallSeverity, "a report identical to baseline has no divergence")
	assert.Equal(t, 0, cmp.CriticalCount)
}

func TestCompareDetectsNewBackendBrand(t *testing.T) {
	baseline := findBaseline("claude-haiku-4-5", "full")
	require.NotNil(t, baseline)
	relay := cloneReport(baseline)
	id := findResult(relay, detectorIdentity)
	require.NotNil(t, id)
	id.Details["detected_non_anthropic_brands"] = []string{"ChatGPT"}

	cmp := compareToBaseline(baseline, relay)
	assert.Equal(t, sevCritical, cmp.OverallSeverity)
	assert.GreaterOrEqual(t, cmp.CriticalCount, 1)
}

func TestCompareDetectsDroppedThinkingSignature(t *testing.T) {
	baseline := findBaseline("claude-haiku-4-5", "full")
	require.NotNil(t, baseline)
	relay := cloneReport(baseline)
	ts := findResult(relay, detectorThinkingSignature)
	require.NotNil(t, ts)
	// baseline saw a thinking block; relay reports none → critical (swapped core).
	ts.Details["thinking_block_seen"] = false

	cmp := compareToBaseline(baseline, relay)
	assert.Equal(t, sevCritical, cmp.OverallSeverity)
	assert.GreaterOrEqual(t, cmp.CriticalCount, 1)
}

func TestCompareDetectsModelMismatch(t *testing.T) {
	baseline := findBaseline("claude-haiku-4-5", "full")
	require.NotNil(t, baseline)
	relay := cloneReport(baseline)
	cons := findResult(relay, detectorConsistency)
	require.NotNil(t, cons)
	cons.Details["model_match"] = false

	cmp := compareToBaseline(baseline, relay)
	assert.Equal(t, sevCritical, cmp.OverallSeverity)
}
