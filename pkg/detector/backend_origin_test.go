package detector

// Backend-origin detector regression tests (ported cc-proxy-detector logic):
// fingerprint classification + the multi-source scoring / missing-field verdict.

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBackendClassifyMsgID(t *testing.T) {
	cases := map[string]string{
		"req_vrtx_abc123":                          "vertex",
		"msg_01PzoexiYoH5j9X4TZWfkx5q":             "anthropic",   // msg_ + base62
		"msg_8a5da866-783c-4dad-9f1e-1234567890ab": "antigravity", // msg_ + UUID
		"8a5da866-783c-4dad-9f1e-1234567890ab":     "rewritten",   // bare UUID
		"chatcmpl-x":                               "rewritten",
		"":                                         "unknown",
	}
	for in, want := range cases {
		assert.Equalf(t, want, boClassifyMsgID(in), "msg_id %q", in)
	}
}

func TestBackendClassifyThinkingSig(t *testing.T) {
	assert.Equal(t, "none", boClassifyThinkingSig(""))
	assert.Equal(t, "short", boClassifyThinkingSig(shortStr(50)))
	assert.Equal(t, "vertex", boClassifyThinkingSig("claude#"+shortStr(200)))
	assert.Equal(t, "normal", boClassifyThinkingSig(shortStr(250)))
}

func shortStr(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

func TestBackendAnalyzeVerdicts(t *testing.T) {
	// Genuine Anthropic direct: native tool/msg ids + the must-have fields present.
	anthropic := boAnalyze([]backendFingerprint{
		{valid: true, toolIDSource: "anthropic", msgIDSource: "anthropic", hasServiceTier: true, hasInferenceGeo: true, hasCacheObj: true},
		{valid: true, isThinkingProbe: true, thinkingProbed: true, thinkingClass: "normal", thinkingLen: 250, msgIDSource: "anthropic", hasInferenceGeo: true},
	})
	assert.Equal(t, "anthropic", anthropic.verdict)
	assert.False(t, anthropic.suspicious)
	assert.Empty(t, anthropic.missing)

	// Bedrock/Kiro: model field leaks the kiro- prefix (ironclad).
	bedrock := boAnalyze([]backendFingerprint{
		{valid: true, toolIDSource: "bedrock", msgIDSource: "antigravity", modelSource: "kiro", usageCamel: true},
	})
	assert.Equal(t, "bedrock", bedrock.verdict)

	// Vertex/Antigravity: req_vrtx_ id + claude# thinking signature.
	vertex := boAnalyze([]backendFingerprint{
		{valid: true, toolIDSource: "vertex", msgIDSource: "vertex", isThinkingProbe: true, thinkingProbed: true, thinkingClass: "vertex", thinkingLen: 120},
	})
	assert.Equal(t, "antigravity", vertex.verdict)

	// Spoofed Anthropic: rewritten toolu_ + injected service_tier, but the
	// unforgeable fields (inference_geo / cache_creation / thinking signature) are
	// all missing → unmasked as suspicious.
	spoof := boAnalyze([]backendFingerprint{
		{valid: true, toolIDSource: "anthropic", hasServiceTier: true},
		{valid: true, toolIDSource: "anthropic", hasServiceTier: true, isThinkingProbe: true, thinkingProbed: true, thinkingClass: "none", thinkingLen: 0},
	})
	assert.Equal(t, "suspicious", spoof.verdict)
	assert.True(t, spoof.suspicious)
	assert.Len(t, spoof.missing, 3)

	// No usable fingerprints → unknown (detector self-skips).
	unknown := boAnalyze([]backendFingerprint{{valid: false}})
	assert.Equal(t, "unknown", unknown.verdict)
}
