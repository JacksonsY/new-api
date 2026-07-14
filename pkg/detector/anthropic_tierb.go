package detector

// Tier-B Anthropic detectors ported from Veridrop (AGPL-3.0):
// knowledge / behavioral_signature / pdf. Each uses bundled static data
// (see embed.go) and needs no per-model baseline.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// --- knowledge (knowledge_questions.json) ---
// Ported from protocols/anthropic/detectors/knowledge.py.

// numberedAnswerRe matches "1. answer", "2) answer", "3: answer", "4 - answer".
var numberedAnswerRe = regexp.MustCompile(`^\s*(\d+)\s*[.):\-]\s*(.+?)\s*$`)

func knowledgeApplies(q knowledgeQuestion, model string) bool {
	if len(q.ApplicableModels) == 0 {
		return true
	}
	for _, m := range q.ApplicableModels {
		if strings.HasPrefix(model, m) || strings.HasPrefix(m, model) {
			return true
		}
	}
	return false
}

// gradeKnowledge returns true if the answer hits expected keywords AND avoids
// anti keywords. Per-model expected_by_model overrides the global list.
func gradeKnowledge(answer string, q knowledgeQuestion, model string) bool {
	if answer == "" {
		return false
	}
	a := strings.ToLower(answer)
	for _, k := range q.AntiKeywords {
		if strings.Contains(a, strings.ToLower(k)) {
			return false
		}
	}
	var expected []string
	for mKey, kws := range q.ExpectedByModel {
		if strings.HasPrefix(model, mKey) || strings.HasPrefix(mKey, model) {
			expected = kws
			break
		}
	}
	if expected == nil {
		expected = q.ExpectedKeywords
	}
	if len(expected) == 0 {
		return true
	}
	if q.ExpectedKeywordMatch == "any" {
		for _, k := range expected {
			if strings.Contains(a, strings.ToLower(k)) {
				return true
			}
		}
		return false
	}
	for _, k := range expected {
		if !strings.Contains(a, strings.ToLower(k)) {
			return false
		}
	}
	return true
}

func buildCombinedKnowledgePrompt(coverage []knowledgeQuestion) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Please answer these %d questions briefly. Reply with one short answer "+
		"per line, prefixed by question number (e.g. '1. <answer>'). If you don't know an "+
		"answer, reply 'unknown' for that line — do not guess.", len(coverage))
	for i, q := range coverage {
		fmt.Fprintf(&b, "\n\n%d. %s", i+1, q.Prompt)
	}
	return b.String()
}

func parseNumberedAnswers(text string, n int) map[int]string {
	out := make(map[int]string)
	for _, line := range strings.Split(text, "\n") {
		m := numberedAnswerRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		idx := 0
		fmt.Sscanf(m[1], "%d", &idx)
		if idx >= 1 && idx <= n {
			out[idx] = m[2]
		}
	}
	return out
}

func anthropicKnowledge(ctx context.Context, p *prober, cfg Config) DetectorResult {
	questions, err := loadKnowledgeQuestions()
	if err != nil {
		return detectorSkip("knowledge questions unavailable: " + err.Error())
	}
	var applicable []knowledgeQuestion
	for _, q := range questions {
		if knowledgeApplies(q, cfg.Model) {
			applicable = append(applicable, q)
		}
	}
	if len(applicable) == 0 {
		return detectorSkip("no applicable questions for model")
	}
	var critical, coverage []knowledgeQuestion
	for _, q := range applicable {
		if q.Type == "critical" {
			critical = append(critical, q)
		} else {
			coverage = append(coverage, q)
		}
	}

	type perQ struct {
		ID     string `json:"id"`
		Passed bool   `json:"passed"`
		Answer string `json:"answer"`
	}
	var per []perQ

	// critical questions: one request each (not bundled).
	for _, q := range critical {
		res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), map[string]interface{}{
			"model": cfg.Model, "max_tokens": 60, "temperature": 0,
			"messages": []map[string]interface{}{{"role": "user", "content": q.Prompt}},
		})
		ans := ""
		if res.err == nil && res.ok() {
			ans = anthropicText(res.parsed)
		}
		per = append(per, perQ{q.ID, gradeKnowledge(ans, q, cfg.Model), truncate(ans, 200)})
	}

	// coverage questions: one combined request.
	if len(coverage) > 0 {
		res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), map[string]interface{}{
			"model": cfg.Model, "max_tokens": 400, "temperature": 0,
			"messages": []map[string]interface{}{{"role": "user", "content": buildCombinedKnowledgePrompt(coverage)}},
		})
		if res.err != nil || !res.ok() {
			for _, q := range coverage {
				per = append(per, perQ{q.ID, false, ""})
			}
		} else {
			parsed := parseNumberedAnswers(anthropicText(res.parsed), len(coverage))
			for i, q := range coverage {
				a := parsed[i+1]
				per = append(per, perQ{q.ID, gradeKnowledge(a, q, cfg.Model), truncate(a, 200)})
			}
		}
	}

	passes := 0
	for _, r := range per {
		if r.Passed {
			passes++
		}
	}
	total := len(per)
	score := 0.0
	if total > 0 {
		score = float64(passes) / float64(total) * 100.0
	}
	status := "fail"
	if score >= 70 {
		status = "pass"
	}
	return DetectorResult{Status: status, Score: score, Details: map[string]interface{}{
		"passes": passes, "total": total, "per_question": per,
	}}
}

// --- behavioral_signature (behavioral_signatures.json) ---
// Ported from protocols/anthropic/detectors/behavioral_signature.py.

// evaluateSignature hits when no unexpected pattern matches AND expected match
// (all/any). Patterns were pre-compiled with (?im) in embed.go.
func evaluateSignature(text string, sig compiledSignature) bool {
	for _, re := range sig.unexpected {
		if re.MatchString(text) {
			return false
		}
	}
	if len(sig.expected) == 0 {
		return true
	}
	if sig.matchMode == "any" {
		for _, re := range sig.expected {
			if re.MatchString(text) {
				return true
			}
		}
		return false
	}
	for _, re := range sig.expected {
		if !re.MatchString(text) {
			return false
		}
	}
	return true
}

func anthropicBehavioral(ctx context.Context, p *prober, cfg Config) DetectorResult {
	sigs, err := loadBehavioralSignatures()
	if err != nil || len(sigs) == 0 {
		return detectorSkip("no signatures loaded")
	}
	type sigResult struct {
		ID      string  `json:"id"`
		Hit     bool    `json:"hit"`
		Weight  float64 `json:"weight"`
		Excerpt string  `json:"response_excerpt"`
	}
	var results []sigResult
	weightedHits, maxWeight := 0.0, 0.0
	for _, sig := range sigs {
		maxWeight += sig.weight
		payload := map[string]interface{}{
			"model": cfg.Model, "max_tokens": 350, "temperature": 0,
			"messages": []map[string]interface{}{{"role": "user", "content": sig.prompt}},
		}
		if sig.system != "" {
			payload["system"] = sig.system
		}
		res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), payload)
		if res.err != nil || !res.ok() {
			results = append(results, sigResult{sig.id, false, sig.weight, ""})
			continue
		}
		text := anthropicText(res.parsed)
		hit := evaluateSignature(text, sig)
		results = append(results, sigResult{sig.id, hit, sig.weight, truncate(text, 200)})
		if hit {
			weightedHits += sig.weight
		}
	}
	score := 0.0
	if maxWeight > 0 {
		score = weightedHits / maxWeight * 100.0
	}
	hits := 0
	for _, r := range results {
		if r.Hit {
			hits++
		}
	}
	status := "fail"
	if score >= 70 {
		status = "pass"
	}
	return DetectorResult{Status: status, Score: score, Details: map[string]interface{}{
		"signatures": results, "hits": hits, "total": len(results),
	}}
}

// --- pdf (test_document.pdf) ---
// Ported from protocols/anthropic/detectors/pdf.py. Anti-阉割: a relay that
// forwards documents returns the magic string; one that strips multimodal fails.

const pdfMagic = "MAGIC-7F3K-VERIFY-CLAUDE-RELAY-DETECTOR"

func anthropicPDF(ctx context.Context, p *prober, cfg Config) DetectorResult {
	pdfB64, err := loadTestPDFBase64()
	if err != nil {
		return detectorSkip("test PDF unavailable: " + err.Error())
	}
	payload := map[string]interface{}{
		"model": cfg.Model, "max_tokens": 150, "temperature": 0,
		"messages": []map[string]interface{}{{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{
					"type": "document",
					"source": map[string]interface{}{
						"type": "base64", "media_type": "application/pdf", "data": pdfB64,
					},
				},
				map[string]interface{}{"type": "text", "text": "What unique identifier string appears in this document? Reply with only the identifier string and no other text."},
			},
		}},
	}
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), payload)
	details := map[string]interface{}{"status_code": res.statusCode, "magic_string": pdfMagic}
	if res.err != nil {
		return DetectorResult{Status: "fail", Score: 0, Error: res.err.Error(), Details: details}
	}
	if !res.ok() {
		msg := upstreamErrorText(res)
		details["error"] = msg
		return DetectorResult{Status: "fail", Score: 0, Error: msg, Details: details}
	}
	text := anthropicText(res.parsed)
	score, note := 0.0, "empty_response"
	if strings.Contains(text, pdfMagic) {
		score, note = 100.0, "magic_found"
	} else if strings.TrimSpace(text) != "" {
		score, note = 50.0, "responded_but_missed_magic"
	}
	details["response_text"] = truncate(text, 300)
	details["evaluation"] = note
	status := "fail"
	if score >= 70 {
		status = "pass"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}
