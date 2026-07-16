package detector

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBoDetectProxyPlatform(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
		want   string
	}{
		{"cloudflare", http.Header{"Cf-Aig-Cache-Status": {"HIT"}}, "Cloudflare AI Gateway"},
		{"litellm", http.Header{"X-Litellm-Version": {"1.0"}}, "LiteLLM"},
		{"helicone", http.Header{"Helicone-Id": {"abc"}}, "Helicone"},
		{"portkey", http.Header{"X-Portkey-Trace-Id": {"t1"}}, "Portkey"},
		{"kong", http.Header{"X-Kong-Request-Id": {"k1"}}, "Kong"},
		{"dashscope", http.Header{"X-Dashscope-Sk": {"x"}}, "Alibaba DashScope"},
		{"azure", http.Header{"Apim-Request-Id": {"a1"}}, "Azure AI Foundry"},
		{"openrouter_key", http.Header{"X-Generation-Id": {"gen-1"}}, "OpenRouter"},
		{"newapi", http.Header{"X-New-Api-Version": {"v1"}}, "New-API"},
		{"oneapi", http.Header{"X-Oneapi-Request-Id": {"1"}}, "One-API"},
		{"aidistri", http.Header{"X-Aidistri-Node": {"n1"}}, "Aidistri"},
		{"openrouter_value", http.Header{"Server": {"openrouter"}}, "OpenRouter"},
		{"none", http.Header{"Content-Type": {"application/json"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, boDetectProxyPlatform(c.header))
		})
	}
}
