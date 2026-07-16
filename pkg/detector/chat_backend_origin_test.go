package detector

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChatBackendHeaderClassifiers(t *testing.T) {
	// OpenAI infrastructure headers (org / version / processing-ms).
	assert.True(t, chatHasOpenAIInfraHeaders(http.Header{"Openai-Organization": {"org-abc"}}))
	assert.True(t, chatHasOpenAIInfraHeaders(http.Header{"Openai-Version": {"2020-10-01"}}))
	assert.False(t, chatHasOpenAIInfraHeaders(http.Header{"Content-Type": {"application/json"}}))

	// Azure OpenAI (APIM gateway / x-ms-* management headers).
	assert.True(t, chatHasAzureHeaders(http.Header{"Apim-Request-Id": {"a1"}}))
	assert.True(t, chatHasAzureHeaders(http.Header{"X-Ms-Region": {"eastus"}}))
	assert.False(t, chatHasAzureHeaders(http.Header{"Content-Type": {"application/json"}}))
}

func TestExtractChatBackendSignals(t *testing.T) {
	res := httpResult{
		header: http.Header{"Openai-Organization": {"org-abc"}, "X-Litellm-Version": {"1.0"}},
		parsed: map[string]interface{}{
			"id":                 "chatcmpl-123",
			"object":             "chat.completion",
			"system_fingerprint": "fp_44709d6fcb",
		},
	}
	sig := extractChatBackendSignals(res)
	assert.True(t, sig.hasFpPrefix)
	assert.True(t, sig.openaiInfraHdrs)
	assert.False(t, sig.azureHdrs)
	assert.Equal(t, "LiteLLM", sig.gateway)
	assert.Equal(t, "chatcmpl-123", sig.responseID)

	// A proxy that stripped everything leaves no positive signals (still valid,
	// just "unknown backend" — never treated as fakery).
	bare := httpResult{header: http.Header{}, parsed: map[string]interface{}{"id": "x", "object": "chat.completion"}}
	sigBare := extractChatBackendSignals(bare)
	assert.False(t, sigBare.hasFpPrefix)
	assert.False(t, sigBare.openaiInfraHdrs)
	assert.False(t, sigBare.azureHdrs)
	assert.Equal(t, "", sigBare.gateway)
}
