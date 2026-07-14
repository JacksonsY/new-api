package detector

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// Issue severities (align with Veridrop's ok/minor/major/critical scale).
const (
	sevMinor    = "minor"
	sevMajor    = "major"
	sevCritical = "critical"
)

// newIssue builds an issue entry for a detector's details.issues list.
func newIssue(severity, code, message string) map[string]interface{} {
	return map[string]interface{}{"severity": severity, "code": code, "message": message}
}

// attachIssues records the issues and a critical_issue_count on a details map.
// The scorer reads both to apply the critical veto.
func attachIssues(details map[string]interface{}, issues []map[string]interface{}) {
	if len(issues) == 0 {
		return
	}
	details["issues"] = issues
	critical := 0
	for _, i := range issues {
		if asString(i["severity"]) == sevCritical {
			critical++
		}
	}
	details["critical_issue_count"] = critical
}

// hasCriticalIssue reports whether any issue in the list is critical.
func hasCriticalIssue(issues []map[string]interface{}) bool {
	for _, i := range issues {
		if asString(i["severity"]) == sevCritical {
			return true
		}
	}
	return false
}

// Required identity patterns for a genuine Claude relay.
var (
	reClaude    = regexp.MustCompile(`(?i)\bclaude\b`)
	reAnthropic = regexp.MustCompile(`(?i)\banthropic\b`)
)

// brandPattern maps a non-official brand regex to its display label. Ported
// verbatim from Veridrop's identity.py: matching any of these in an identity
// answer flags a non-Anthropic backend (impersonation / swapped core).
type brandPattern struct {
	re    *regexp.Regexp
	label string
}

// brandPatterns 逐条对齐 identity.py NON_ANTHROPIC_BRAND_PATTERNS（17 条）。
// 注：Go RE2 的 \b 只认 ASCII 词界，对 CJK 无效，故「文心」用字面匹配。
var brandPatterns = []brandPattern{
	// AWS family
	{regexp.MustCompile(`(?i)\bamazon\s+q\b`), "Amazon Q"},
	{regexp.MustCompile(`(?i)\baws\b`), "AWS"},
	{regexp.MustCompile(`(?i)\bbedrock\b`), "AWS Bedrock"},
	{regexp.MustCompile(`(?i)\bkiro\b`), "Kiro"},
	// OpenAI family
	{regexp.MustCompile(`(?i)\bchatgpt\b`), "ChatGPT"},
	{regexp.MustCompile(`(?i)\bopenai\b`), "OpenAI"},
	{regexp.MustCompile(`(?i)\bgpt[-\s]?[345](\.\d+)?\b`), "GPT-3/4/5"},
	// Google family
	{regexp.MustCompile(`(?i)\bgemini\b`), "Gemini"},
	{regexp.MustCompile(`(?i)\bbard\b`), "Bard"},
	// Microsoft family
	{regexp.MustCompile(`(?i)\bcopilot\b`), "Copilot"},
	// Open-source / 国内
	{regexp.MustCompile(`(?i)\bdeepseek\b`), "DeepSeek"},
	{regexp.MustCompile(`(?i)\bqwen\b`), "Qwen"},
	{regexp.MustCompile(`(?i)\btongyi\b`), "Tongyi"},
	{regexp.MustCompile(`(?i)\bdoubao\b`), "Doubao"},
	{regexp.MustCompile(`(?i)\bwenxin\b|文心`), "Wenxin"},
	{regexp.MustCompile(`(?i)\bllama\b`), "LLaMA"},
	{regexp.MustCompile(`(?i)\bmistral\b`), "Mistral"},
}

// scanBrands returns the distinct non-official brand labels found in text, in
// table order.
func scanBrands(text string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, bp := range brandPatterns {
		if bp.re.MatchString(text) && !seen[bp.label] {
			seen[bp.label] = true
			out = append(out, bp.label)
		}
	}
	return out
}

// modelMatches reports whether a returned model name is consistent with the
// requested one, tolerating dated/aliased suffixes (e.g. requested
// "claude-3-5-sonnet" vs returned "claude-3-5-sonnet-20241022").
// normalizeModelID 对齐真源码 _normalize_model_id：小写、trim、剥 gemini 的
// "models/" 前缀、把 '.'/'_' 归一为 '-'，避免 "claude-sonnet-4.5" vs
// "claude-sonnet-4-5-…" 或 "models/gemini-2.5-flash" 误判不匹配。
func normalizeModelID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "models/")
	s = strings.NewReplacer(".", "-", "_", "-").Replace(s)
	return s
}

func modelMatches(requested, returned string) bool {
	r := normalizeModelID(requested)
	g := normalizeModelID(returned)
	if g == "" {
		return false
	}
	if r == "" {
		return true
	}
	if r == g {
		return true
	}
	return strings.HasPrefix(g, r) || strings.HasPrefix(r, g)
}

// clampScore keeps a score within [0, 100].
func clampScore(s float64) float64 {
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}

// detectorSkip returns a skipped result carrying a reason. The runner fills in
// name/weight; skipped detectors are excluded from the weighted average.
func detectorSkip(reason string) DetectorResult {
	return DetectorResult{Status: "skip", Score: 0, Details: map[string]interface{}{"reason": reason}}
}

// sortedKeys returns the keys of a set (map[string]bool) in sorted order, for
// stable issue/violation lists in passive-detector details.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// intField reads a JSON number field as an int (JSON decodes numbers to
// float64). Returns (0,false) when absent or non-numeric; bools never match.
func intField(m map[string]interface{}, key string) (int, bool) {
	f, ok := numField(m, key)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// isNonNegInt mirrors protocol.py _is_nonneg_int: a whole number >= 0 (JSON
// numbers decode to float64, so we also require an integral value).
func isNonNegInt(v interface{}) bool {
	f, ok := v.(float64)
	return ok && f >= 0 && f == math.Trunc(f)
}

// stringRatio is a Go port of rapidfuzz.fuzz.ratio: the normalized indel
// (LCS-based) similarity of two strings as a 0..100 percentage, used by the
// integrity detector to compare streamed vs non-streamed text. Operates on
// runes so multibyte text compares correctly.
func stringRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 && len(rb) == 0 {
		return 100
	}
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	lcs := runeLCS(ra, rb)
	return 2.0 * float64(lcs) / float64(len(ra)+len(rb)) * 100.0
}

// runeLCS returns the longest-common-subsequence length using a rolling 1-D DP
// (O(n·m) time, O(min) space). Probe texts are short, so this is cheap.
func runeLCS(a, b []rune) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else if prev[j] >= curr[j-1] {
				curr[j] = prev[j]
			} else {
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
