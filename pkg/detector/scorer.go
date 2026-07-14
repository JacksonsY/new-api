package detector

import "strings"

// This file is a direct port of Veridrop's core/scorer.py (AGPL-3.0). The
// thresholds and the critical-veto behavior are reproduced verbatim.

// verdictFor maps a 0..100 score to a report-level verdict.
// (scorer.py: verdict_for)
func verdictFor(score float64) string {
	if score >= 70.0 {
		return "passed"
	}
	if score >= 50.0 {
		return "marginal"
	}
	return "failed"
}

// resultHasCritical reports whether a single detector surfaced one or more
// critical-severity issues, via details.critical_issue_count > 0 or via
// details.issues[*].severity == "critical". Skipped detectors never count.
func resultHasCritical(r DetectorResult) bool {
	if r.Status == "skip" || r.Details == nil {
		return false
	}
	if count, ok := r.Details["critical_issue_count"]; ok {
		if toInt(count) > 0 {
			return true
		}
	}
	switch issues := r.Details["issues"].(type) {
	case []map[string]interface{}:
		for _, issue := range issues {
			if asString(issue["severity"]) == "critical" {
				return true
			}
		}
	case []interface{}:
		for _, raw := range issues {
			if issue, ok := raw.(map[string]interface{}); ok && asString(issue["severity"]) == "critical" {
				return true
			}
		}
	}
	return false
}

// hasCriticalIssues reports whether any detector surfaced a critical issue.
// (scorer.py: has_critical_issues)
func hasCriticalIssues(results []DetectorResult) bool {
	for _, r := range results {
		if resultHasCritical(r) {
			return true
		}
	}
	return false
}

// criticalCount is the number of detectors carrying at least one critical issue.
func criticalCount(results []DetectorResult) int {
	n := 0
	for _, r := range results {
		if resultHasCritical(r) {
			n++
		}
	}
	return n
}

// effectiveVerdict is verdict_for(score) capped by critical findings: a
// critical issue downgrades a "passed" base verdict to "marginal" so a
// high structural score cannot hide a fundamental fake.
// (scorer.py: effective_verdict)
func effectiveVerdict(score float64, results []DetectorResult) string {
	base := verdictFor(score)
	if hasCriticalIssues(results) && base == "passed" {
		return "marginal"
	}
	return base
}

// computeTotal is the weighted average of scores over non-skipped detectors:
// Σ(score*weight)/Σ(weight); 0 when there is no effective weight.
// (scorer.py: compute_total)
func computeTotal(results []DetectorResult) float64 {
	weightSum := 0.0
	weighted := 0.0
	for _, r := range results {
		if r.Status == "skip" {
			continue
		}
		weightSum += r.Weight
		weighted += r.Score * r.Weight
	}
	if weightSum <= 0 {
		return 0.0
	}
	return weighted / weightSum
}

// summaryText mirrors scorer.py's summary_text (Chinese labels).
func summaryText(score float64, verdict string) string {
	switch verdict {
	case "passed":
		if score >= 85.0 {
			return "优秀"
		}
		return "通过"
	case "marginal":
		return "基本合格"
	default:
		return "未达标"
	}
}

// fatalUpstreamPatterns / fatalModelUnavailablePatterns are ported verbatim
// from scorer.py so a quota/billing/model-unavailable failure upstream is
// reported as an invalid run rather than a low model-quality score.
var fatalUpstreamPatterns = []string{
	"credit balance is too low",
	"specified api usage limits",
	"usage limits",
	"insufficient_quota",
	"billing",
}

var fatalModelUnavailablePatterns = []string{
	"model_not_found",
	"model not found",
	"model_not_available",
	"model not available",
	"no available channel",
	"无可用渠道",
}

// fatalRunError returns a run-level error message when the collected detector
// errors indicate upstream quota/billing exhaustion or an unavailable model.
// (scorer.py: fatal_run_error)
func fatalRunError(results []DetectorResult) string {
	var haystacks []string
	for _, r := range results {
		if r.Error != "" {
			haystacks = append(haystacks, r.Error)
		}
		collectNestedErrors(r.Details, &haystacks)
	}
	if len(haystacks) == 0 {
		return ""
	}
	text := strings.ToLower(strings.Join(haystacks, "\n"))
	for _, p := range fatalUpstreamPatterns {
		if strings.Contains(text, p) {
			return "检测无效: 上游返回余额不足或用量限制错误。请更换有额度的 API key，或降低检测模式/模型后重新检测。"
		}
	}
	for _, p := range fatalModelUnavailablePatterns {
		if strings.Contains(text, p) {
			return "检测无效: 中转站没有当前模型的可用渠道，或该模型名称不被此中转站支持。请更换该站已开通的模型后重新检测。"
		}
	}
	return ""
}

// collectNestedErrors walks details maps/slices collecting any string under an
// "error" key. (scorer.py: _collect_nested_errors)
func collectNestedErrors(value interface{}, out *[]string) {
	switch v := value.(type) {
	case map[string]interface{}:
		if err, ok := v["error"].(string); ok {
			*out = append(*out, err)
		}
		for _, child := range v {
			collectNestedErrors(child, out)
		}
	case []interface{}:
		for _, child := range v {
			collectNestedErrors(child, out)
		}
	case []map[string]interface{}:
		for _, child := range v {
			collectNestedErrors(child, out)
		}
	}
}
