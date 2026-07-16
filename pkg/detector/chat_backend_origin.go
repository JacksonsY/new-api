package detector

// chat_backend_origin.go gives the OpenAI/Gemini protocols a backend/gateway
// origin detector, the chat-surface analogue of anthropic backend_origin
// (cc-proxy-detector). Unlike Claude — where tool-id/msg-id/thinking-signature
// prefixes forge a rich fingerprint matrix — the OpenAI-compatible surface has
// few hard backend tells, and a legitimate relay (new-api included) routinely
// strips system_fingerprint and openai-* headers. So MISSING authenticity
// signals is never treated as fakery; the detector records the backend + gateway
// software as evidence and passes. It fails only on a clean CONTRADICTION: a
// response that claims to be Gemini while emitting OpenAI-exclusive
// infrastructure headers (openai-organization / openai-version) a genuine Google
// endpoint cannot produce — i.e. an OpenAI-family backend served as Gemini. The
// gateway table is the one ported from LLMprobe-engine channel-signature.ts
// (AGPL-3.0); attribution retained, additive, does not replace any new-api /
// QuantumNous identity.

import (
	"context"
	"net/http"
	"strings"
)

// chatOpenAIInfraHeaders are headers only OpenAI's own infrastructure emits (the
// org id and API version are account/infra-scoped; a Google or generic backend
// has no equivalent). Their presence identifies an OpenAI-family backend.
func chatHasOpenAIInfraHeaders(h http.Header) bool {
	return h.Get("openai-organization") != "" || h.Get("openai-version") != "" || h.Get("openai-processing-ms") != ""
}

// chatHasAzureHeaders detects Azure OpenAI (APIM gateway + Azure ML session /
// x-ms-* management headers).
func chatHasAzureHeaders(h http.Header) bool {
	if h.Get("apim-request-id") != "" || h.Get("azureml-model-session") != "" {
		return true
	}
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "x-ms-") {
			return true
		}
	}
	return false
}

type chatBackendSignals struct {
	gateway         string
	systemFinger    string
	hasFpPrefix     bool
	responseID      string
	object          string
	openaiInfraHdrs bool
	azureHdrs       bool
}

func extractChatBackendSignals(res httpResult) chatBackendSignals {
	sig := chatBackendSignals{
		gateway:         boDetectProxyPlatform(res.header),
		openaiInfraHdrs: chatHasOpenAIInfraHeaders(res.header),
		azureHdrs:       chatHasAzureHeaders(res.header),
	}
	if res.parsed != nil {
		sig.systemFinger = strField(res.parsed, "system_fingerprint")
		sig.hasFpPrefix = strings.HasPrefix(sig.systemFinger, "fp_")
		sig.responseID = strField(res.parsed, "id")
		sig.object = strField(res.parsed, "object")
	}
	return sig
}

// chatBackendOrigin records the backend provider + gateway software as evidence
// (passing, because a proxy stripping authenticity signals is normal), and fails
// only on a Gemini-claiming response that leaks OpenAI infrastructure headers.
// Full-mode.
func chatBackendOrigin(ctx context.Context, p *prober, cfg Config) DetectorResult {
	// A plain chat probe is enough: system_fingerprint, id, object and the
	// backend/gateway headers are on the envelope even if a reasoning model
	// returns empty text. Routed to the protocol's native surface (gemini →
	// generateContent), so the response headers are the real backend's; native
	// Gemini simply carries no OpenAI system_fingerprint/object (backend stays
	// "未知(剥离)"), while an OpenAI-family backend served as Gemini still leaks
	// its infra headers and trips the contradiction check below.
	res := p.probeChat(ctx, cfg.Model, "Reply with exactly: ok", 64)
	if res.err != nil || !res.ok() {
		return detectorSkip("backend-origin probe could not run")
	}
	sig := extractChatBackendSignals(res)

	backend := "未知(中转已剥离后端指纹)"
	switch {
	case sig.azureHdrs:
		backend = "Azure OpenAI"
	case sig.hasFpPrefix || sig.openaiInfraHdrs:
		backend = "OpenAI 官方 / 兼容直连"
	}

	details := map[string]interface{}{"backend": backend, "response_object": sig.object}
	summary := "后端: " + backend
	if sig.gateway != "" {
		details["gateway_software"] = sig.gateway
		summary += " · 网关软件: " + sig.gateway
	}
	details["summary"] = summary
	if sig.systemFinger != "" {
		details["system_fingerprint"] = sig.systemFinger
	}
	if sig.responseID != "" {
		details["response_id"] = sig.responseID
	}

	// Contradiction: a genuine Google endpoint never emits OpenAI's org/version
	// infrastructure headers. Claiming Gemini while emitting them means an
	// OpenAI-family backend is being served as Gemini (model substitution).
	if p.protocol == ProtocolGemini && sig.openaiInfraHdrs {
		attachIssues(details, []map[string]interface{}{newIssue(sevMajor, "backend_substitution",
			"声称 Gemini 却泄漏 OpenAI 独有基础设施头(openai-organization / openai-version)——疑似拿 OpenAI 系后端冒充 Gemini")})
		return DetectorResult{Status: "fail", Score: 30, Details: details}
	}
	return DetectorResult{Status: "pass", Score: 100, Details: details}
}
