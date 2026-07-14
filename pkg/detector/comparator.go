package detector

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// Baseline comparison (Veridrop core/comparator_framework.py +
// protocols/anthropic/comparator.py). A relay's report is diffed against a
// per-model ground-truth baseline captured from the official API. This is where
// the *relative* critical signals live — signature dropped vs baseline, a new
// backend brand, model mismatch, tool_use stripped, PDF magic lost — the ones a
// single absolute run cannot emit without a reference.

const sevOK = "ok"

var severityRank = map[string]int{sevOK: 0, sevMinor: 1, sevMajor: 2, sevCritical: 3}

func maxSeverity(a, b string) string {
	if severityRank[b] > severityRank[a] {
		return b
	}
	return a
}

// DetectorComparisonDTO is one detector's baseline-vs-relay diff (report DTO).
type DetectorComparisonDTO struct {
	Name          string   `json:"name"`
	DisplayName   string   `json:"display_name"`
	BaselineScore float64  `json:"baseline_score"`
	RelayScore    float64  `json:"relay_score"`
	ScoreDiff     float64  `json:"score_diff"`
	Severity      string   `json:"severity"`
	Findings      []string `json:"findings,omitempty"`
}

// BaselineComparison is the report block summarizing the whole diff.
type BaselineComparison struct {
	BaselineModel   string                  `json:"baseline_model"`
	OverallSeverity string                  `json:"overall_severity"`
	Summary         string                  `json:"summary"`
	CriticalCount   int                     `json:"critical_count"`
	Detectors       []DetectorComparisonDTO `json:"detectors"`
}

// --- detail accessors (baseline JSON decodes arrays to []interface{}; relay
// details may hold []string — both are handled) ---

func detailStringList(d map[string]interface{}, key string) []string {
	switch v := d[key].(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func detailFloat(d map[string]interface{}, key string) (float64, bool) {
	switch v := d[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

func detailBool(d map[string]interface{}, key string) (bool, bool) {
	b, ok := d[key].(bool)
	return b, ok
}

func detailMap(d map[string]interface{}, key string) map[string]interface{} {
	m, _ := d[key].(map[string]interface{})
	return m
}

// stringSetDiff returns elements in got not in base, sorted.
func stringSetDiff(got, base []string) []string {
	baseSet := map[string]bool{}
	for _, s := range base {
		baseSet[s] = true
	}
	var out []string
	for _, s := range got {
		if !baseSet[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// --- per-detector diff rules (bd = baseline details, rd = relay details) ---

func cmpThinkingSignature(bd, rd map[string]interface{}) (string, []string) {
	sev := sevOK
	var f []string
	bSeen, _ := detailBool(bd, "thinking_block_seen")
	rSeen, _ := detailBool(rd, "thinking_block_seen")
	if bSeen && !rSeen {
		f = append(f, "thinking 块完全没返回 — 中转站可能未启用 thinking,或后台模型不支持")
		sev = sevCritical
	}
	bSig, _ := detailFloat(bd, "signature_length")
	rSig, _ := detailFloat(rd, "signature_length")
	if bSig > 0 && rSig == 0 {
		f = append(f, fmt.Sprintf("signature 缺失 (baseline 长度 %d chars)", int(bSig)))
		sev = sevCritical
	} else if bSig > 0 && rSig > 0 && rSig < bSig*0.3 {
		f = append(f, fmt.Sprintf("signature 异常短: relay %d chars vs baseline %d", int(rSig), int(bSig)))
		sev = maxSeverity(sev, sevMajor)
	}
	return sev, f
}

func cmpIdentity(bd, rd map[string]interface{}) (string, []string) {
	sev := sevOK
	var f []string
	bReq := len(detailStringList(bd, "required_hits"))
	rReq := len(detailStringList(rd, "required_hits"))
	if bReq > rReq {
		f = append(f, fmt.Sprintf("身份关键词命中减少: baseline %d/2, relay %d/2", bReq, rReq))
		sev = sevMajor
	}
	if comp := detailStringList(rd, "competitor_hits"); len(comp) > 0 {
		f = append(f, fmt.Sprintf("出现竞品关键词: %v", comp))
		sev = sevCritical
	}
	newBrands := stringSetDiff(detailStringList(rd, "detected_non_anthropic_brands"), detailStringList(bd, "detected_non_anthropic_brands"))
	if len(newBrands) > 0 {
		f = append(f, "⚠ 检测到非 Anthropic 后端品牌: "+strings.Join(newBrands, ", "))
		sev = sevCritical
	}
	return sev, f
}

func cmpConsistency(bd, rd map[string]interface{}) (string, []string) {
	sev := sevOK
	var f []string
	if m, ok := detailBool(rd, "model_match"); ok && !m {
		f = append(f, "response.model 不匹配请求")
		sev = sevCritical
	}
	bCV, ok1 := detailFloat(bd, "stability_cv")
	rCV, ok2 := detailFloat(rd, "stability_cv")
	if ok1 && ok2 {
		label := ""
		switch {
		case rCV > 0.30:
			label = "高度不稳定"
		case rCV > 0.10:
			label = "可疑波动"
		case rCV > bCV*2+0.03:
			label = "略有波动"
		}
		if label != "" {
			f = append(f, fmt.Sprintf("输出稳定性 %s: relay CV=%.3f vs baseline CV=%.3f", label, rCV, bCV))
			if rCV > 0.30 {
				sev = maxSeverity(sev, sevMajor)
			} else {
				sev = maxSeverity(sev, sevMinor)
			}
		}
	}
	return sev, f
}

func cmpBehavioral(bd, rd map[string]interface{}) (string, []string) {
	bHits, _ := detailFloat(bd, "hits")
	rHits, _ := detailFloat(rd, "hits")
	if bHits > rHits {
		return sevMinor, []string{fmt.Sprintf("行为指纹缺失: baseline hits=%d → relay hits=%d", int(bHits), int(rHits))}
	}
	return sevOK, nil
}

func cmpKnowledge(bd, rd map[string]interface{}) (string, []string) {
	bP, _ := detailFloat(bd, "passes")
	rP, _ := detailFloat(rd, "passes")
	if bP > rP {
		return sevMinor, []string{fmt.Sprintf("知识题通过减少: baseline %d → relay %d", int(bP), int(rP))}
	}
	return sevOK, nil
}

func cmpPDF(bd, rd map[string]interface{}) (string, []string) {
	if strField(bd, "evaluation") == "magic_found" && strField(rd, "evaluation") != "magic_found" {
		return sevCritical, []string{fmt.Sprintf("PDF 识别失败: relay 评估=%q (baseline=magic_found)", strField(rd, "evaluation"))}
	}
	return sevOK, nil
}

func cmpStructuredOutput(bd, rd map[string]interface{}) (string, []string) {
	bBlocks := detailStringList(bd, "content_block_types")
	rBlocks := detailStringList(rd, "content_block_types")
	if contains(bBlocks, "tool_use") && !contains(rBlocks, "tool_use") {
		f := []string{fmt.Sprintf("tool_use 块缺失 — relay 仅返回 %v, stop_reason=%q", rBlocks, strField(rd, "stop_reason"))}
		if text := strField(rd, "text_response"); strings.TrimSpace(text) != "" {
			f = append(f, "  模型实际回复: "+truncate(strings.TrimSpace(text), 200))
		}
		return sevCritical, f
	}
	bSub := detailMap(bd, "sub_checks")
	rSub := detailMap(rd, "sub_checks")
	var failed []string
	for key, bc := range bSub {
		bm, _ := bc.(map[string]interface{})
		rm := detailMap(rSub, key)
		bp, _ := detailBool(bm, "pass")
		rp, _ := detailBool(rm, "pass")
		if bp && !rp {
			failed = append(failed, fmt.Sprintf("%s=%v", key, rm["value"]))
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		sev := sevMinor
		for _, d := range failed {
			if strings.HasPrefix(d, "id_prefix=") {
				sev = sevMajor
			}
		}
		return sev, []string{"tool_use 子检查失败: " + strings.Join(failed, ", ")}
	}
	return sevOK, nil
}

func cmpIntegrity(bd, rd map[string]interface{}) (string, []string) {
	bSub := detailMap(bd, "sub_checks")
	rSub := detailMap(rd, "sub_checks")
	var failed []string
	for key, bc := range bSub {
		bm, _ := bc.(map[string]interface{})
		rm := detailMap(rSub, key)
		bp, _ := detailBool(bm, "pass")
		rp, _ := detailBool(rm, "pass")
		if bp && !rp {
			failed = append(failed, key)
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return sevMinor, []string{"integrity 子检查失败: " + strings.Join(failed, "; ")}
	}
	return sevOK, nil
}

func cmpProtocol(bd, rd map[string]interface{}) (string, []string) {
	newIssues := stringSetDiff(detailStringList(rd, "issues"), detailStringList(bd, "issues"))
	if len(newIssues) > 0 {
		sample := newIssues
		if len(sample) > 3 {
			sample = sample[:3]
		}
		return sevMinor, []string{fmt.Sprintf("协议偏差 %d 项: %s", len(newIssues), strings.Join(sample, ", "))}
	}
	return sevOK, nil
}

func cmpMessageID(bd, rd map[string]interface{}) (string, []string) {
	newV := stringSetDiff(detailStringList(rd, "violations"), detailStringList(bd, "violations"))
	if len(newV) == 0 {
		return sevOK, nil
	}
	sev := sevMinor
	if contains(newV, "id_prefix_invalid") {
		sev = sevMajor
	}
	return sev, []string{"ID 前缀违规: " + strings.Join(newV, ", ")}
}

var perDetectorRules = map[string]func(bd, rd map[string]interface{}) (string, []string){
	detectorThinkingSignature: cmpThinkingSignature,
	detectorIdentity:          cmpIdentity,
	detectorBehavioral:        cmpBehavioral,
	detectorConsistency:       cmpConsistency,
	detectorKnowledge:         cmpKnowledge,
	detectorPDF:               cmpPDF,
	detectorStructuredOutput:  cmpStructuredOutput,
	detectorIntegrity:         cmpIntegrity,
	detectorProtocol:          cmpProtocol,
	detectorMessageID:         cmpMessageID,
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// compareOne builds one detector's comparison (either side may be nil).
func compareOne(name string, b, r *DetectorResult) DetectorComparisonDTO {
	var bScore, rScore float64
	var displayName string
	if b != nil {
		bScore = b.Score
		displayName = b.DisplayName
	}
	if r != nil {
		rScore = r.Score
		if r.DisplayName != "" {
			displayName = r.DisplayName
		}
	}
	if displayName == "" {
		displayName = name
	}
	diff := rScore - bScore
	sev := sevOK
	var findings []string

	switch {
	case b == nil && r != nil:
		findings = append(findings, "baseline 缺少该检测项")
		sev = sevMinor
	case r == nil && b != nil:
		findings = append(findings, "relay 报告缺少该检测项 (检测可能 skipped)")
		sev = sevMajor
	case b != nil && r != nil:
		if r.Status == "skip" && b.Status != "skip" {
			findings = append(findings, "relay 跳过该检测项")
			sev = sevMajor
		} else if r.Status == "error" {
			findings = append(findings, "relay 检测出错: "+truncate(r.Error, 120))
			sev = sevMajor
		} else if rule := perDetectorRules[name]; rule != nil {
			s2, f2 := rule(b.Details, r.Details)
			sev = maxSeverity(sev, s2)
			findings = append(findings, f2...)
		}
	}

	if len(findings) == 0 && (diff >= 10 || diff <= -10) {
		if diff < -25 {
			sev = maxSeverity(sev, sevMajor)
		} else if diff < 0 {
			sev = maxSeverity(sev, sevMinor)
		}
		findings = append(findings, fmt.Sprintf("分数差距 %+.0f (无具体差异定位)", diff))
	}

	return DetectorComparisonDTO{
		Name: name, DisplayName: displayName,
		BaselineScore: bScore, RelayScore: rScore, ScoreDiff: diff,
		Severity: sev, Findings: findings,
	}
}

// compareToBaseline diffs a relay report against a baseline report.
func compareToBaseline(baseline, relay *Report) *BaselineComparison {
	bIdx := indexResults(baseline)
	rIdx := indexResults(relay)
	names := make([]string, 0, len(bIdx))
	seen := map[string]bool{}
	for _, res := range baseline.Results {
		if !seen[res.Name] {
			seen[res.Name] = true
			names = append(names, res.Name)
		}
	}
	for _, res := range relay.Results {
		if !seen[res.Name] {
			seen[res.Name] = true
			names = append(names, res.Name)
		}
	}

	var detectors []DetectorComparisonDTO
	overall := sevOK
	critCount := 0
	for _, n := range names {
		cmp := compareOne(n, bIdx[n], rIdx[n])
		detectors = append(detectors, cmp)
		overall = maxSeverity(overall, cmp.Severity)
		if cmp.Severity == sevCritical {
			critCount++
		}
	}

	return &BaselineComparison{
		BaselineModel:   baseline.TargetModel,
		OverallSeverity: overall,
		Summary:         comparisonSummary(baseline, relay, overall, detectors),
		CriticalCount:   critCount,
		Detectors:       detectors,
	}
}

func indexResults(rep *Report) map[string]*DetectorResult {
	idx := map[string]*DetectorResult{}
	for i := range rep.Results {
		r := &rep.Results[i]
		idx[r.Name] = r
	}
	return idx
}

func comparisonSummary(baseline, relay *Report, overall string, detectors []DetectorComparisonDTO) string {
	if !modelMatches(baseline.TargetModel, relay.TargetModel) {
		return fmt.Sprintf("模型不一致: baseline=%s, relay=%s — 无法可靠对比", baseline.TargetModel, relay.TargetModel)
	}
	if overall == sevOK {
		return fmt.Sprintf("中转站行为与官方基线一致 (总分 %.1f vs %.1f)", relay.TotalScore, baseline.TotalScore)
	}
	crit, major, minor := 0, 0, 0
	for _, d := range detectors {
		switch d.Severity {
		case sevCritical:
			crit++
		case sevMajor:
			major++
		case sevMinor:
			minor++
		}
	}
	parts := []string{fmt.Sprintf("总分 %.1f vs baseline %.1f (%+.1f)", relay.TotalScore, baseline.TotalScore, relay.TotalScore-baseline.TotalScore)}
	var bits []string
	if crit > 0 {
		bits = append(bits, fmt.Sprintf("%d 项严重 (critical)", crit))
	}
	if major > 0 {
		bits = append(bits, fmt.Sprintf("%d 项重大 (major)", major))
	}
	if minor > 0 {
		bits = append(bits, fmt.Sprintf("%d 项轻微 (minor)", minor))
	}
	if len(bits) > 0 {
		parts = append(parts, strings.Join(bits, "; "))
	}
	if overall == sevCritical {
		parts = append(parts, "中转站极有可能不是声称的 Claude 模型")
	} else if overall == sevMajor {
		parts = append(parts, "中转站存在能力剥离或协议显著偏差")
	}
	return strings.Join(parts, " — ")
}

// --- baseline auto-discovery (embedded) ---

var (
	baselinesOnce  sync.Once
	baselinesCache []*Report
)

// baselineDoc is a lenient view of a baseline JSON. It takes only the fields the
// comparator needs; notably it ignores `timestamp` (an ISO string in the
// Veridrop baselines, vs the Report's int64 unix) and `performance`, so those
// type differences don't fail the load.
type baselineDoc struct {
	TargetModel          string           `json:"target_model"`
	Mode                 string           `json:"mode"`
	TotalScore           float64          `json:"total_score"`
	Verdict              string           `json:"verdict"`
	Results              []DetectorResult `json:"results"`
	SelfReportedIdentity string           `json:"self_reported_identity"`
}

func loadBaselines() []*Report {
	baselinesOnce.Do(func() {
		entries, err := dataFS.ReadDir("data/baselines")
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			raw, err := dataFS.ReadFile("data/baselines/" + e.Name())
			if err != nil {
				continue
			}
			var doc baselineDoc
			if common.Unmarshal(raw, &doc) != nil {
				continue
			}
			baselinesCache = append(baselinesCache, &Report{
				TargetModel:          doc.TargetModel,
				Mode:                 doc.Mode,
				TotalScore:           doc.TotalScore,
				Verdict:              doc.Verdict,
				Results:              doc.Results,
				SelfReportedIdentity: doc.SelfReportedIdentity,
			})
		}
	})
	return baselinesCache
}

// findBaseline returns the embedded ground-truth baseline whose model+mode
// matches, or nil. Baselines are full-mode Claude reports, so comparison only
// applies to a full-mode anthropic run of a known model.
func findBaseline(model, mode string) *Report {
	for _, b := range loadBaselines() {
		if b.Mode == mode && modelMatches(model, b.TargetModel) {
			return b
		}
	}
	return nil
}
