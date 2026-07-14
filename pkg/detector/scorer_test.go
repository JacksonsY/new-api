package detector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func passResult(score, weight float64) DetectorResult {
	return DetectorResult{Status: "pass", Score: score, Weight: weight}
}

func skipResult(weight float64) DetectorResult {
	return DetectorResult{Status: "skip", Score: 0, Weight: weight}
}

func criticalByCount() DetectorResult {
	return DetectorResult{
		Status: "fail", Score: 0, Weight: 1,
		Details: map[string]interface{}{"critical_issue_count": 1},
	}
}

func criticalByIssues() DetectorResult {
	return DetectorResult{
		Status: "fail", Score: 0, Weight: 1,
		Details: map[string]interface{}{
			"issues": []map[string]interface{}{{"severity": "critical", "code": "x"}},
		},
	}
}

func TestVerdictFor(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{100, "passed"},
		{70, "passed"},
		{69.999, "marginal"},
		{50, "marginal"},
		{49.999, "failed"},
		{0, "failed"},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, verdictFor(c.score), "verdictFor(%v)", c.score)
	}
}

func TestEffectiveVerdictCriticalCap(t *testing.T) {
	cases := []struct {
		name    string
		score   float64
		results []DetectorResult
		want    string
	}{
		{"passed no critical", 75, []DetectorResult{passResult(75, 1)}, "passed"},
		// The core invariant: a high score with a critical issue is capped to marginal.
		{"passed with critical capped", 75, []DetectorResult{passResult(75, 1), criticalByCount()}, "marginal"},
		{"passed with issues-critical capped", 90, []DetectorResult{criticalByIssues()}, "marginal"},
		{"marginal stays marginal", 60, []DetectorResult{passResult(60, 1), criticalByCount()}, "marginal"},
		{"failed stays failed", 40, []DetectorResult{passResult(40, 1), criticalByCount()}, "failed"},
		{"excellent no critical", 95, []DetectorResult{passResult(95, 1)}, "passed"},
		// A skipped detector carrying critical markers must NOT trigger the cap.
		{"skip critical ignored", 80, []DetectorResult{passResult(80, 1),
			{Status: "skip", Details: map[string]interface{}{"critical_issue_count": 3}}}, "passed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, effectiveVerdict(c.score, c.results))
		})
	}
}

func TestComputeTotal(t *testing.T) {
	t.Run("weighted average excludes skip", func(t *testing.T) {
		results := []DetectorResult{
			passResult(100, 2),
			passResult(50, 1),
			skipResult(5), // excluded from both numerator and denominator
		}
		// (100*2 + 50*1) / (2+1) = 250/3
		assert.InDelta(t, 250.0/3.0, computeTotal(results), 1e-9)
	})

	t.Run("all skipped is zero", func(t *testing.T) {
		assert.Equal(t, 0.0, computeTotal([]DetectorResult{skipResult(1), skipResult(2)}))
	})

	t.Run("zero effective weight is zero", func(t *testing.T) {
		assert.Equal(t, 0.0, computeTotal([]DetectorResult{passResult(100, 0)}))
	})

	t.Run("empty is zero", func(t *testing.T) {
		assert.Equal(t, 0.0, computeTotal(nil))
	})
}

func TestCriticalCount(t *testing.T) {
	results := []DetectorResult{
		passResult(100, 1),
		criticalByCount(),
		criticalByIssues(),
		{Status: "skip", Details: map[string]interface{}{"critical_issue_count": 9}}, // skipped: not counted
	}
	assert.Equal(t, 2, criticalCount(results))
	assert.True(t, hasCriticalIssues(results))
}

func TestResultHasCritical(t *testing.T) {
	t.Run("count as float64 (json-decoded)", func(t *testing.T) {
		r := DetectorResult{Status: "fail", Details: map[string]interface{}{"critical_issue_count": float64(2)}}
		assert.True(t, resultHasCritical(r))
	})
	t.Run("issues as []interface{} (json-decoded)", func(t *testing.T) {
		r := DetectorResult{Status: "fail", Details: map[string]interface{}{
			"issues": []interface{}{map[string]interface{}{"severity": "critical"}},
		}}
		assert.True(t, resultHasCritical(r))
	})
	t.Run("non-critical issue is not critical", func(t *testing.T) {
		r := DetectorResult{Status: "fail", Details: map[string]interface{}{
			"issues": []map[string]interface{}{{"severity": "major"}},
		}}
		assert.False(t, resultHasCritical(r))
	})
	t.Run("nil details", func(t *testing.T) {
		assert.False(t, resultHasCritical(DetectorResult{Status: "pass"}))
	})
}

func TestSummaryText(t *testing.T) {
	assert.Equal(t, "优秀", summaryText(90, "passed"))
	assert.Equal(t, "通过", summaryText(72, "passed"))
	assert.Equal(t, "基本合格", summaryText(55, "marginal"))
	assert.Equal(t, "未达标", summaryText(10, "failed"))
}

func TestFatalRunError(t *testing.T) {
	t.Run("billing", func(t *testing.T) {
		results := []DetectorResult{{Status: "error", Error: "Your credit balance is too low to access the API"}}
		require.NotEmpty(t, fatalRunError(results))
	})
	t.Run("model unavailable nested", func(t *testing.T) {
		results := []DetectorResult{{Status: "error", Details: map[string]interface{}{"error": "model_not_found"}}}
		require.NotEmpty(t, fatalRunError(results))
	})
	t.Run("ordinary failure is not fatal", func(t *testing.T) {
		results := []DetectorResult{{Status: "fail", Error: "response id malformed"}}
		require.Empty(t, fatalRunError(results))
	})
}
