package detector

import (
	"embed"
	"encoding/base64"
	"regexp"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// Tier-B bundled static data, ported from Veridrop
// (relay_detector/protocols/anthropic/data/, AGPL-3.0). These files are
// embedded so the single Go binary needs no external assets.
//
//go:embed data/knowledge_questions.json data/behavioral_signatures.json data/test_document.pdf data/baselines
var dataFS embed.FS

// --- knowledge questions (knowledge_questions.json) ---

type knowledgeQuestion struct {
	ID                   string              `json:"id"`
	Type                 string              `json:"type"` // "coverage" | "critical"
	Prompt               string              `json:"prompt"`
	ExpectedKeywords     []string            `json:"expected_keywords"`
	ExpectedKeywordMatch string              `json:"expected_keyword_match"` // "all" (default) | "any"
	ExpectedByModel      map[string][]string `json:"expected_by_model"`
	AntiKeywords         []string            `json:"anti_keywords"`
	ApplicableModels     []string            `json:"applicable_models"`
}

var (
	knowledgeOnce  sync.Once
	knowledgeCache []knowledgeQuestion
	knowledgeErr   error
)

func loadKnowledgeQuestions() ([]knowledgeQuestion, error) {
	knowledgeOnce.Do(func() {
		raw, err := dataFS.ReadFile("data/knowledge_questions.json")
		if err != nil {
			knowledgeErr = err
			return
		}
		var doc struct {
			Questions []knowledgeQuestion `json:"questions"`
		}
		if err := common.Unmarshal(raw, &doc); err != nil {
			knowledgeErr = err
			return
		}
		knowledgeCache = doc.Questions
	})
	return knowledgeCache, knowledgeErr
}

// --- behavioral signatures (behavioral_signatures.json) ---

type behavioralSignatureData struct {
	ID                 string   `json:"id"`
	Prompt             string   `json:"prompt"`
	System             string   `json:"system"`
	ExpectedPatterns   []string `json:"expected_patterns"`
	UnexpectedPatterns []string `json:"unexpected_patterns"`
	ExpectedMatch      string   `json:"expected_match"` // "all" (default) | "any"
	Weight             float64  `json:"weight"`
}

// compiledSignature is a behavioral signature with its patterns pre-compiled.
// Patterns use Python's re.IGNORECASE|re.MULTILINE, reproduced with "(?im)".
type compiledSignature struct {
	id         string
	prompt     string
	system     string
	weight     float64
	matchMode  string
	expected   []*regexp.Regexp
	unexpected []*regexp.Regexp
}

var (
	behavioralOnce  sync.Once
	behavioralCache []compiledSignature
	behavioralErr   error
)

// compileSignaturePattern compiles a stored pattern with case-insensitive,
// multiline semantics. Patterns that fail to compile are dropped (and logged)
// rather than aborting the whole detector.
func compileSignaturePattern(pattern string) *regexp.Regexp {
	re, err := regexp.Compile("(?im)" + pattern)
	if err != nil {
		common.SysError("detector: bad behavioral signature pattern " + pattern + ": " + err.Error())
		return nil
	}
	return re
}

func loadBehavioralSignatures() ([]compiledSignature, error) {
	behavioralOnce.Do(func() {
		raw, err := dataFS.ReadFile("data/behavioral_signatures.json")
		if err != nil {
			behavioralErr = err
			return
		}
		var doc struct {
			Signatures []behavioralSignatureData `json:"signatures"`
		}
		if err := common.Unmarshal(raw, &doc); err != nil {
			behavioralErr = err
			return
		}
		for _, sig := range doc.Signatures {
			weight := sig.Weight
			if weight == 0 {
				weight = 1.0
			}
			cs := compiledSignature{
				id:        sig.ID,
				prompt:    sig.Prompt,
				system:    sig.System,
				weight:    weight,
				matchMode: sig.ExpectedMatch,
			}
			for _, p := range sig.ExpectedPatterns {
				if re := compileSignaturePattern(p); re != nil {
					cs.expected = append(cs.expected, re)
				}
			}
			for _, p := range sig.UnexpectedPatterns {
				if re := compileSignaturePattern(p); re != nil {
					cs.unexpected = append(cs.unexpected, re)
				}
			}
			behavioralCache = append(behavioralCache, cs)
		}
	})
	return behavioralCache, behavioralErr
}

// --- test PDF (test_document.pdf) ---

var (
	pdfOnce  sync.Once
	pdfB64   string
	pdfBytes []byte
	pdfErr   error
)

// loadTestPDFBase64 returns the embedded test PDF as a standard base64 string
// for an Anthropic document/base64 content block.
func loadTestPDFBase64() (string, error) {
	pdfOnce.Do(func() {
		data, err := dataFS.ReadFile("data/test_document.pdf")
		if err != nil {
			pdfErr = err
			return
		}
		pdfBytes = data
		pdfB64 = base64.StdEncoding.EncodeToString(data)
	})
	return pdfB64, pdfErr
}
