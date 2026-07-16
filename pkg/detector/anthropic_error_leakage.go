package detector

// anthropic_error_leakage.go ports Step 9 (error-response leakage) of
// api-relay-audit (github.com/toby-bridges/api-relay-audit, AGPL-3.0). It fires
// deterministic broken requests and scans the error response body + headers for
// echoed credentials, upstream provider URLs, environment-variable names,
// server-side filesystem paths, stack traces, and LiteLLM router internals — all
// signs a relay is bleeding its internal plumbing to clients (AC-2 credential
// exposure, the most common relay abuse). Attribution retained per the AGPL;
// additive, complements error_shape (which only checks the error envelope shape).

import (
	"context"
	"net/http"
	"strings"
)

var errLeakUpstreamHosts = []string{
	"api.anthropic.com", "api.openai.com", "generativelanguage.googleapis.com",
	"openrouter.ai", "api.mistral.ai", "api.deepseek.com", "api.together.xyz", "api.groq.com",
}
var errLeakEnvMarkers = []string{
	"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY", "GEMINI_API_KEY",
	"DEEPSEEK_API_KEY", "MISTRAL_API_KEY", "API_KEY=", "SECRET_KEY=",
}
var errLeakPathPrefixes = []string{
	"/home/", "/root/", "/var/www/", "/var/lib/", "/app/", "/opt/", "/usr/local/",
	`C:\Users\`, `C:\ProgramData\`,
}
var errLeakStackMarkers = []string{
	"Traceback (most recent call last)", `File "`, "at <anonymous>", "at Object.",
	"at async ", "goroutine 1 [", "panic: ",
}
var errLeakLitellmMarkers = []string{"litellm_params", "litellm_call_id", "model_list", "litellm.exceptions"}

func sevRank(s string) int {
	switch s {
	case "critical":
		return 3
	case "high":
		return 2
	case "medium":
		return 1
	}
	return 0
}

// scanErrorLeakage classifies internal-plumbing leaks in an error body + headers.
// api_key echo / env-var dump = critical (credential exposure); upstream URL =
// high; filesystem path / stack trace / LiteLLM internals = medium.
func scanErrorLeakage(body string, header http.Header, apiKey string) (sev string, hits []string) {
	hay := body
	for _, vals := range header {
		hay += " " + strings.Join(vals, " ")
	}
	sev = "none"
	bump := func(s string) {
		if sevRank(s) > sevRank(sev) {
			sev = s
		}
	}
	if len(apiKey) >= 12 && strings.Contains(hay, apiKey) {
		hits = append(hits, "api_key_echoed")
		bump("critical")
	}
	for _, m := range errLeakEnvMarkers {
		if strings.Contains(hay, m) {
			hits = append(hits, "env_var_leak:"+m)
			bump("critical")
		}
	}
	for _, h := range errLeakUpstreamHosts {
		if strings.Contains(hay, h) {
			hits = append(hits, "upstream_url:"+h)
			bump("high")
		}
	}
	for _, pfx := range errLeakPathPrefixes {
		if strings.Contains(body, pfx) {
			hits = append(hits, "path_leak:"+pfx)
			bump("medium")
		}
	}
	for _, m := range errLeakStackMarkers {
		if strings.Contains(body, m) {
			hits = append(hits, "stack_trace")
			bump("medium")
			break
		}
	}
	for _, m := range errLeakLitellmMarkers {
		if strings.Contains(body, m) {
			hits = append(hits, "litellm_internal:"+m)
			bump("medium")
			break
		}
	}
	return sev, hits
}

// errorLeakTriggers are deterministic broken requests (path + body) that should
// elicit a clean error from a genuine endpoint.
var errorLeakTriggers = []struct {
	path string
	body map[string]interface{}
}{
	{anthropicMessagesPath, map[string]interface{}{"model": "definitely-not-a-real-model-zzz", "max_tokens": 4, "messages": []map[string]interface{}{{"role": "user", "content": "hi"}}}},
	{anthropicMessagesPath, map[string]interface{}{"model": "", "max_tokens": 4}}, // missing messages
	{"/v1/this-endpoint-does-not-exist", map[string]interface{}{"x": 1}},
}

// anthropicErrorLeakage fires broken requests and fails if any error response
// leaks internal plumbing.
func anthropicErrorLeakage(ctx context.Context, p *prober, cfg Config) DetectorResult {
	worst, allHits, probed := "none", []string{}, 0
	for _, trig := range errorLeakTriggers {
		res := p.postJSON(ctx, trig.path, anthropicHeaders(p.apiKey), trig.body)
		if res.err != nil {
			continue
		}
		probed++
		sev, hits := scanErrorLeakage(res.text, res.header, p.apiKey)
		if sevRank(sev) > sevRank(worst) {
			worst = sev
		}
		allHits = append(allHits, hits...)
	}
	if probed == 0 {
		return detectorSkip("error-leakage probes could not run")
	}

	details := map[string]interface{}{"leaks": allHits, "severity": worst}
	switch worst {
	case "critical":
		attachIssues(details, []map[string]interface{}{newIssue(sevCritical, "error_credential_leak",
			"错误响应里泄漏了凭证(回显 key / dump 环境变量)——中转在向客户端暴露内部凭证")})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	case "high":
		attachIssues(details, []map[string]interface{}{newIssue(sevMajor, "error_upstream_leak",
			"错误响应里泄漏了上游 provider URL——中转在暴露内部转发链路")})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	case "medium":
		attachIssues(details, []map[string]interface{}{newIssue(sevMinor, "error_internal_leak",
			"错误响应里泄漏了服务端路径 / 堆栈 / LiteLLM 内部字段(未密封的异常路径)")})
		return DetectorResult{Status: "fail", Score: 50, Details: details}
	default:
		return DetectorResult{Status: "pass", Score: 100, Details: details}
	}
}
