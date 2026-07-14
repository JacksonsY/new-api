package detector

// Tier-B port regression: embedded data loads + knowledge/behavioral grading logic.

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedTierBDataLoads(t *testing.T) {
	qs, err := loadKnowledgeQuestions()
	require.NoError(t, err)
	require.NotEmpty(t, qs, "knowledge_questions.json 应非空")
	assert.NotEmpty(t, qs[0].Prompt)

	sigs, err := loadBehavioralSignatures()
	require.NoError(t, err)
	require.NotEmpty(t, sigs, "behavioral_signatures.json 应非空")

	pdf, err := loadTestPDFBase64()
	require.NoError(t, err)
	assert.NotEmpty(t, pdf, "test PDF base64 应非空")
}

func TestGradeKnowledge(t *testing.T) {
	q := knowledgeQuestion{
		ExpectedKeywords: []string{"dario", "amodei"},
		AntiKeywords:     []string{"unknown"},
	}
	assert.True(t, gradeKnowledge("Dario Amodei", q, "claude-x"), "全部关键词命中")
	assert.False(t, gradeKnowledge("Dario", q, "claude-x"), "all 模式缺一个应失败")
	assert.False(t, gradeKnowledge("unknown", q, "claude-x"), "命中 anti 关键词应失败")
	assert.False(t, gradeKnowledge("", q, "claude-x"), "空答案应失败")

	qAny := knowledgeQuestion{
		ExpectedKeywords:     []string{"principles", "values", "rules"},
		ExpectedKeywordMatch: "any",
	}
	assert.True(t, gradeKnowledge("based on a set of values", qAny, "claude-x"), "any 模式命中一个即可")
	assert.False(t, gradeKnowledge("something else entirely", qAny, "claude-x"), "any 模式一个都不命中")

	qNone := knowledgeQuestion{}
	assert.True(t, gradeKnowledge("whatever", qNone, "claude-x"), "无期望关键词则通过")
}

func TestParseNumberedAnswers(t *testing.T) {
	text := "1. Dario Amodei\n2) Daniela Amodei\n3: Constitutional AI\n9. out of range"
	got := parseNumberedAnswers(text, 3)
	assert.Equal(t, "Dario Amodei", got[1])
	assert.Equal(t, "Daniela Amodei", got[2])
	assert.Equal(t, "Constitutional AI", got[3])
	_, ok := got[9]
	assert.False(t, ok, "超出题数的行应忽略")
}

func TestEvaluateSignature(t *testing.T) {
	sig := compiledSignature{
		matchMode:  "all",
		expected:   []*regexp.Regexp{compileSignaturePattern("hello"), compileSignaturePattern("world")},
		unexpected: []*regexp.Regexp{compileSignaturePattern("forbidden")},
	}
	assert.True(t, evaluateSignature("hello world", sig), "expected 全命中且无 unexpected")
	assert.False(t, evaluateSignature("hello world forbidden", sig), "命中 unexpected 应失败")
	assert.False(t, evaluateSignature("hello there", sig), "all 模式缺 world 应失败")

	anySig := compiledSignature{
		matchMode: "any",
		expected:  []*regexp.Regexp{compileSignaturePattern("foo"), compileSignaturePattern("bar")},
	}
	assert.True(t, evaluateSignature("just bar", anySig), "any 模式命中一个即可")
	assert.False(t, evaluateSignature("neither here", anySig), "any 模式都不命中")
}
