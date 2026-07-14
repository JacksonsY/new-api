package detector

// Phase C 回归：长上下文大海捞针——needle 确定性、haystack 嵌入、召回计数、
// 分档聚合（pass/partial/fail），以及"真截断被判 fail"的端到端（假 send 闭包，无网络）。

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var lcAnswerRE = regexp.MustCompile(`\b[A-Z]+-[0-9A-F]{4}-[0-9A-F]{4}\b`)

func TestLongContextNeedlesAndHaystack(t *testing.T) {
	needles := makeNeedles("seedA")
	require.Len(t, needles, 3)
	// deterministic by seed
	assert.Equal(t, needles[0].answer, makeNeedles("seedA")[0].answer)
	assert.NotEqual(t, needles[0].answer, makeNeedles("seedB")[0].answer)

	hay := assembleHaystack(2000, needles, "seedA", "openai")
	for _, n := range needles {
		assert.Contains(t, hay, n.answer, "haystack must embed every needle")
	}
	// haystack roughly hits the char target (2000 tok × 6.0 ≈ 12000 chars).
	assert.Greater(t, len(hay), 8000)

	all := strings.Join([]string{needles[0].answer, needles[1].answer, needles[2].answer}, "\n")
	assert.Equal(t, 3, countRecalls(all, needles))
	assert.Equal(t, 1, countRecalls(needles[0].answer, needles))
	assert.Equal(t, 0, countRecalls("nothing here", needles))
}

func TestModelContextLimit(t *testing.T) {
	assert.Equal(t, 1_000_000, modelContextLimit("claude-opus-4-8-20260101"))
	assert.Equal(t, 200_000, modelContextLimit("claude-haiku-4-5"))
	assert.Equal(t, 128_000, modelContextLimit("gpt-4o-mini"))
	assert.Equal(t, 128_000, modelContextLimit("totally-unknown-model"))
}

func TestAggregateLongContext(t *testing.T) {
	s, st, _ := aggregateLongContext([]lcTier{{32000, 3, 3, "pass"}, {100000, 3, 3, "pass"}})
	assert.Equal(t, 100.0, s)
	assert.Equal(t, "pass", st)

	// a fail tier is strong truncation evidence → status fail.
	s, st, _ = aggregateLongContext([]lcTier{{32000, 3, 3, "pass"}, {100000, 1, 3, "fail"}})
	assert.Equal(t, "fail", st)
	assert.Equal(t, 50.0, s)

	// partial (2/3) is a soft signal → status stays pass, score reflects 66.
	s, st, _ = aggregateLongContext([]lcTier{{32000, 3, 3, "pass"}, {100000, 2, 3, "partial"}})
	assert.Equal(t, "pass", st)
	assert.InDelta(t, 83.0, s, 0.01)

	// nothing conclusive probed → skip.
	_, st, _ = aggregateLongContext([]lcTier{{200000, 0, 3, "skip"}})
	assert.Equal(t, "skip", st)
	_, st, _ = aggregateLongContext([]lcTier{{32000, 0, 3, "rate_limited"}})
	assert.Equal(t, "skip", st)
}

func TestLongContextExtremeTiers(t *testing.T) {
	// Adaptive tiers cover ~32k / 50% / 95% of the model's window.
	assert.Equal(t, []int{32_000, 500_000, 950_000}, tiersForModel(1_000_000))
	assert.Equal(t, []int{32_000, 100_000, 190_000}, tiersForModel(200_000))
	for _, tier := range tiersForModel(16_385) {
		assert.GreaterOrEqual(t, tier, 1_000) // small models still yield valid tiers
	}

	// Extreme opt-in enables long-context even without the standard flag, and
	// uses the adaptive strategy.
	cfg := Config{BaseURL: "https://relay.test", Model: "claude-haiku-4-5", Protocol: "anthropic", IncludeLongContextExtreme: true}
	honest := func(prompt string) (string, bool, string) {
		return strings.Join(lcAnswerRE.FindAllString(prompt, -1), "\n"), true, ""
	}
	res := runLongContext(cfg, "anthropic", honest)
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, "extreme", res.Details["tier_strategy"])

	// The gate: extreme flag alone must not self-skip.
	got := longContextDetector(nil, &prober{tel: &runTelemetry{}}, Config{Model: "claude-haiku-4-5"})
	assert.Equal(t, "skip", got.Status) // neither flag → skip
}

func TestRunLongContextCatchesTruncation(t *testing.T) {
	cfg := Config{BaseURL: "https://relay.test", Model: "claude-opus-4-8", Protocol: "anthropic", IncludeLongContext: true}

	// Honest relay: echoes every identifier present in the full prompt → recalls
	// all 3 needles at every tier → pass.
	honest := func(prompt string) (string, bool, string) {
		return strings.Join(lcAnswerRE.FindAllString(prompt, -1), "\n"), true, ""
	}
	res := runLongContext(cfg, "anthropic", honest)
	assert.Equal(t, "pass", res.Status)
	assert.Equal(t, 100.0, res.Score)

	// Truncating relay: only "sees" the first 5000 chars, so needles at 50%/90%
	// (and the 10% needle at ~16k chars for the 32k tier) are lost → fail.
	truncating := func(prompt string) (string, bool, string) {
		head := prompt
		if len(head) > 5000 {
			head = head[:5000]
		}
		return strings.Join(lcAnswerRE.FindAllString(head, -1), "\n"), true, ""
	}
	res = runLongContext(cfg, "anthropic", truncating)
	assert.Equal(t, "fail", res.Status)

	// Opt-in gate: without IncludeLongContext the detector self-skips.
	skip := longContextDetector(nil, &prober{tel: &runTelemetry{}}, Config{Model: "claude-opus-4-8"})
	assert.Equal(t, "skip", skip.Status)
}
