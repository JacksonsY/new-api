package detector

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanErrorLeakage(t *testing.T) {
	empty := http.Header{}

	// A clean Anthropic-style error leaks nothing.
	sev, _ := scanErrorLeakage(`{"type":"error","error":{"type":"invalid_request_error","message":"model: unknown model 'x'"}}`, empty, "sk-key")
	assert.Equal(t, "none", sev)

	// Critical: env-var dump / echoed key.
	sev, _ = scanErrorLeakage(`{"error":"OPENAI_API_KEY=sk-abc123 not set"}`, empty, "")
	assert.Equal(t, "critical", sev)
	sev, _ = scanErrorLeakage("invalid key: sk-live-supersecretkey123", empty, "sk-live-supersecretkey123")
	assert.Equal(t, "critical", sev)

	// High: upstream provider URL bled through.
	sev, _ = scanErrorLeakage("upstream request to https://api.anthropic.com/v1/messages failed", empty, "")
	assert.Equal(t, "high", sev)

	// Medium: filesystem path / stack trace / LiteLLM internals.
	sev, _ = scanErrorLeakage("Traceback (most recent call last):\n  File \"/app/router.py\", line 42", empty, "")
	assert.Equal(t, "medium", sev)
	sev, _ = scanErrorLeakage(`{"litellm_params":{"model":"claude"}}`, empty, "")
	assert.Equal(t, "medium", sev)

	// Header-carried upstream leak is caught too.
	sev, _ = scanErrorLeakage("{}", http.Header{"X-Upstream": []string{"api.openai.com"}}, "")
	assert.Equal(t, "high", sev)

	// A short key is not matched (avoid false positives on tiny strings).
	sev, _ = scanErrorLeakage("error code abc", empty, "abc")
	assert.Equal(t, "none", sev)
}
