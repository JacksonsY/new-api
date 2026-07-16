package detector

// backend_origin.go is a native Go port of cc-proxy-detector
// (github.com/zxc123aa/cc-proxy-detector, MIT license, Copyright (c) 2026
// zxc123aa). It answers a question the genuine-vs-fake detectors do not: which
// REAL backend a Claude relay routes to — Anthropic direct, AWS Bedrock (Kiro
// reverse-engineered), or Google Vertex (Antigravity reverse-engineered) — and
// flags a relay that spoofs Anthropic by rewriting tool ids / injecting
// service_tier while it cannot forge the must-have fields (inference_geo,
// cache_creation object, thinking signature).
//
// Attribution is retained per the MIT license and is additive; it does not
// replace any new-api / QuantumNous project identity.

import (
	"context"
	"math"
	"net/http"
	"regexp"
	"strings"
)

// Fingerprint rules (cc-proxy-detector fingerprint matrix).
const (
	boToolPrefixAnthropic = "toolu_"
	boToolPrefixBedrock   = "tooluse_"
	boMsgPrefix           = "msg_"
	boMsgVertexPrefix     = "req_vrtx_"
	boModelPrefixBedrock  = "anthropic."
	boModelPrefixKiro     = "kiro-"
	boThinkingShortLen    = 100 // Antigravity signatures are usually < 100
)

var (
	boVertexToolIDRe = regexp.MustCompile(`^tool_\d+$`)
	boUUIDRe         = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

var boAWSHeaderKW = []string{"x-amzn", "x-amz-", "bedrock"}
var boAnthropicHeaderKW = []string{"anthropic-ratelimit", "x-ratelimit", "retry-after"}

// boClassifyMsgID mirrors classify_msg_id: req_vrtx_ → vertex; msg_ + UUID →
// antigravity (forwarded/forged); msg_ + base62 → anthropic native; else rewritten.
func boClassifyMsgID(msgID string) string {
	if msgID == "" {
		return "unknown"
	}
	if strings.HasPrefix(msgID, boMsgVertexPrefix) {
		return "vertex"
	}
	if strings.HasPrefix(msgID, boMsgPrefix) {
		if boUUIDRe.MatchString(strings.TrimPrefix(msgID, boMsgPrefix)) {
			return "antigravity"
		}
		return "anthropic"
	}
	return "rewritten"
}

// boClassifyThinkingSig mirrors classify_thinking_sig.
func boClassifyThinkingSig(sig string) string {
	if sig == "" {
		return "none"
	}
	if len(sig) < boThinkingShortLen {
		return "short"
	}
	if strings.HasPrefix(sig, "claude#") {
		return "vertex"
	}
	return "normal"
}

// boGatewaySoftware is the Tier-1 relay/gateway-software fingerprint table
// (ported from LLMprobe-engine channel-signature.ts, AGPL-3.0): deterministic
// response-header signatures for the gateway middleware sitting IN FRONT of the
// cloud backend. cc-proxy-detector answers which cloud provider serves the model;
// this answers which relay software forwarded it. Matched in table order
// (specific → generic) for a stable result.
var boGatewaySoftware = []struct {
	name    string
	keySubs []string // any header-key substring present → match
}{
	{"Cloudflare AI Gateway", []string{"cf-aig-"}},
	{"LiteLLM", []string{"x-litellm-"}},
	{"Helicone", []string{"helicone-"}},
	{"Portkey", []string{"x-portkey-"}},
	{"Kong", []string{"x-kong-"}},
	{"Alibaba DashScope", []string{"x-dashscope-"}},
	{"Azure AI Foundry", []string{"apim-request-id"}},
	{"OpenRouter", []string{"x-generation-id", "openrouter"}},
	{"New-API", []string{"x-new-api-version"}},
	{"One-API", []string{"x-oneapi-request-id"}},
	{"OneAPI/NewAPI", []string{"one-api", "new-api"}},
}

// boDetectProxyPlatform identifies the relay/gateway software from response
// headers.
func boDetectProxyPlatform(h http.Header) string {
	lowerKeys := make([]string, 0, len(h))
	for k := range h {
		lowerKeys = append(lowerKeys, strings.ToLower(k))
	}
	hasKey := func(sub string) bool {
		for _, k := range lowerKeys {
			if strings.Contains(k, sub) {
				return true
			}
		}
		return false
	}
	// Highest-specificity platforms first.
	if hasKey("aidistri") {
		return "Aidistri"
	}
	if strings.Contains(strings.ToLower(h.Get("access-control-allow-headers")), "accounthub") {
		return "AccountHub"
	}
	for _, gw := range boGatewaySoftware {
		for _, sub := range gw.keySubs {
			if hasKey(sub) {
				return gw.name
			}
		}
	}
	// OpenRouter also identifies via a header value (not just a key).
	for k := range h {
		if strings.Contains(strings.ToLower(h.Get(k)), "openrouter") {
			return "OpenRouter"
		}
	}
	return ""
}

// backendFingerprint is one probe's extracted signals.
type backendFingerprint struct {
	valid           bool
	isThinkingProbe bool
	toolIDSource    string // anthropic|bedrock|vertex|rewritten|""
	msgIDSource     string // anthropic|antigravity|vertex|rewritten|unknown
	thinkingClass   string // none|short|vertex|normal|""
	thinkingLen     int
	thinkingProbed  bool
	modelSource     string // kiro|bedrock|anthropic|""
	hasServiceTier  bool
	hasInferenceGeo bool
	hasCacheObj     bool
	usageCamel      bool
	hasAWSHeaders   bool
	hasAnthHeaders  bool
	proxyPlatform   string
}

// boExtractFingerprint pulls the backend fingerprints from a probe response
// (both body and headers), mirroring cc-proxy-detector's probe_once.
func boExtractFingerprint(res httpResult, isThinking bool) backendFingerprint {
	fp := backendFingerprint{isThinkingProbe: isThinking}
	if res.err != nil || !res.ok() || res.parsed == nil {
		return fp
	}
	fp.valid = true
	body := res.parsed

	for k := range res.header {
		kl := strings.ToLower(k)
		for _, kw := range boAWSHeaderKW {
			if strings.Contains(kl, kw) {
				fp.hasAWSHeaders = true
			}
		}
		for _, kw := range boAnthropicHeaderKW {
			if strings.Contains(kl, kw) {
				fp.hasAnthHeaders = true
			}
		}
	}
	fp.proxyPlatform = boDetectProxyPlatform(res.header)

	for _, raw := range subSlice(body, "content") {
		blk, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch strField(blk, "type") {
		case "tool_use":
			if fp.toolIDSource == "" {
				id := strField(blk, "id")
				switch {
				case strings.HasPrefix(id, boToolPrefixBedrock):
					fp.toolIDSource = "bedrock"
				case strings.HasPrefix(id, boToolPrefixAnthropic):
					fp.toolIDSource = "anthropic"
				case boVertexToolIDRe.MatchString(id):
					fp.toolIDSource = "vertex"
				case id != "":
					fp.toolIDSource = "rewritten"
				}
			}
		case "thinking":
			if !fp.thinkingProbed {
				fp.thinkingProbed = true
				sig := strField(blk, "signature")
				fp.thinkingLen = len(sig)
				fp.thinkingClass = boClassifyThinkingSig(sig)
			}
		}
	}

	fp.msgIDSource = boClassifyMsgID(strField(body, "id"))

	model := strField(body, "model")
	switch {
	case strings.HasPrefix(model, boModelPrefixKiro):
		fp.modelSource = "kiro"
	case strings.HasPrefix(model, boModelPrefixBedrock):
		fp.modelSource = "bedrock"
	case model != "":
		fp.modelSource = "anthropic"
	}

	if usage := subMap(body, "usage"); usage != nil {
		if _, ok := usage["inputTokens"]; ok {
			fp.usageCamel = true
		}
		if v, ok := usage["service_tier"]; ok && v != nil {
			fp.hasServiceTier = true
		}
		if v, ok := usage["inference_geo"]; ok && v != nil {
			fp.hasInferenceGeo = true
		}
		if _, ok := usage["cache_creation"].(map[string]interface{}); ok {
			fp.hasCacheObj = true
		}
	}
	return fp
}

// boVerdict is the outcome of boAnalyze.
type boVerdict struct {
	verdict    string // anthropic|bedrock|antigravity|suspicious|unknown
	confidence float64
	scores     map[string]int
	missing    []string
	suspicious bool
	platform   string
}

// boAnalyze ports cc-proxy-detector analyze(): weighted multi-source fingerprint
// scoring, the tooluse_ reassignment correction, and the v5.1 missing-field
// negative-evidence pass that unmasks a spoofed Anthropic relay.
func boAnalyze(fps []backendFingerprint) boVerdict {
	scores := map[string]int{"anthropic": 0, "bedrock": 0, "antigravity": 0}
	var valid []backendFingerprint
	platform := ""
	for _, fp := range fps {
		if fp.valid {
			valid = append(valid, fp)
			if platform == "" {
				platform = fp.proxyPlatform
			}
		}
	}
	if len(valid) == 0 {
		return boVerdict{verdict: "unknown", scores: scores, platform: platform}
	}

	for _, fp := range valid {
		switch fp.toolIDSource {
		case "bedrock":
			scores["bedrock"] += 5
		case "anthropic":
			scores["anthropic"] += 5
		case "vertex":
			scores["antigravity"] += 5
		}
		if fp.thinkingProbed && fp.thinkingClass == "vertex" {
			scores["antigravity"] += 5
		}
		switch fp.msgIDSource {
		case "anthropic":
			scores["anthropic"] += 2
		case "vertex":
			scores["antigravity"] += 6
		}
		switch fp.modelSource {
		case "kiro":
			scores["bedrock"] += 8
		case "bedrock":
			scores["bedrock"] += 3
		}
		if fp.hasServiceTier {
			scores["anthropic"] += 3
		}
		if fp.hasInferenceGeo {
			scores["anthropic"] += 2
		}
		if fp.hasCacheObj {
			scores["anthropic"] += 1
		}
		if fp.usageCamel {
			scores["bedrock"] += 2
		}
		if fp.hasAWSHeaders {
			scores["bedrock"] += 3
		}
		if fp.hasAnthHeaders {
			scores["anthropic"] += 2
		}
	}

	// Correction: with no kiro model but a Vertex signal present, tooluse_ points
	// belong to Antigravity, not Bedrock.
	hasKiro := false
	for _, fp := range valid {
		if fp.modelSource == "kiro" {
			hasKiro = true
		}
	}
	if !hasKiro && scores["antigravity"] > 0 && scores["bedrock"] > 0 && scores["antigravity"] >= 4 {
		pts := 0
		for _, fp := range valid {
			if fp.toolIDSource == "bedrock" {
				pts += 5
			}
		}
		scores["antigravity"] += pts
		scores["bedrock"] -= pts
	}

	// Missing-field negative evidence: only when everything positive points to
	// Anthropic. The must-have fields a relay cannot forge are inference_geo, the
	// cache_creation object, and (on a thinking probe) a real signature.
	var missing []string
	if scores["anthropic"] > 0 && scores["bedrock"] == 0 && scores["antigravity"] == 0 {
		anyGeo, anyCache, hasThinkingProbe, anySig := false, false, false, false
		for _, fp := range valid {
			if fp.hasInferenceGeo {
				anyGeo = true
			}
			if fp.hasCacheObj {
				anyCache = true
			}
			if fp.isThinkingProbe {
				hasThinkingProbe = true
				if fp.thinkingLen > 0 {
					anySig = true
				}
			}
		}
		if !anyGeo {
			missing = append(missing, "inference_geo")
			scores["anthropic"] -= 3
		}
		if !anyCache {
			missing = append(missing, "cache_creation")
			scores["anthropic"] -= 2
		}
		if hasThinkingProbe && !anySig {
			missing = append(missing, "thinking_signature")
			scores["anthropic"] -= 3
		}
	}
	for k := range scores {
		if scores[k] < 0 {
			scores[k] = 0
		}
	}

	total := scores["anthropic"] + scores["bedrock"] + scores["antigravity"]
	if total == 0 {
		if len(missing) > 0 {
			return boVerdict{verdict: "suspicious", scores: scores, missing: missing, suspicious: true, platform: platform}
		}
		return boVerdict{verdict: "unknown", scores: scores, platform: platform}
	}
	// Winner with Anthropic-first tie-break (matches Python dict iteration order).
	winner, best := "anthropic", scores["anthropic"]
	for _, k := range []string{"bedrock", "antigravity"} {
		if scores[k] > best {
			winner, best = k, scores[k]
		}
	}
	conf := math.Round(float64(best)/float64(total)*100) / 100
	if winner == "anthropic" && len(missing) >= 2 {
		return boVerdict{verdict: "suspicious", confidence: conf, scores: scores, missing: missing, suspicious: true, platform: platform}
	}
	return boVerdict{verdict: winner, confidence: conf, scores: scores, missing: missing, platform: platform}
}

// backendOriginLabel maps a verdict to the human-facing backend name.
var backendOriginLabel = map[string]string{
	"anthropic":   "Anthropic 官方 API",
	"bedrock":     "AWS Bedrock (Kiro 逆向)",
	"antigravity": "Google Vertex (Antigravity 逆向)",
	"suspicious":  "疑似伪装 Anthropic",
	"unknown":     "无法确定",
}

// anthropicBackendOrigin runs two tool probes + one thinking probe, extracts the
// backend fingerprints, and reports which real backend the relay routes to. A
// spoofed-Anthropic verdict emits a critical issue; a genuine alternate backend
// (Bedrock/Vertex) passes with the origin recorded as evidence.
func anthropicBackendOrigin(ctx context.Context, p *prober, cfg Config) DetectorResult {
	var fps []backendFingerprint
	for i := 0; i < 2; i++ {
		res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), boToolPayload(cfg.Model))
		if res.err == nil && !res.ok() {
			if msg := upstreamErrorText(res); msg != "" {
				return DetectorResult{Status: "error", Score: 0, Error: msg}
			}
		}
		fps = append(fps, boExtractFingerprint(res, false))
	}
	if modelSupportsThinking(cfg.Model) {
		res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), boThinkingPayload(cfg.Model))
		fps = append(fps, boExtractFingerprint(res, true))
	}

	v := boAnalyze(fps)
	if v.verdict == "unknown" {
		return detectorSkip("no usable backend fingerprints")
	}

	details := map[string]interface{}{
		"backend":    backendOriginLabel[v.verdict],
		"verdict":    v.verdict,
		"confidence": v.confidence,
		"scores":     v.scores,
	}
	summary := "后端: " + backendOriginLabel[v.verdict]
	if v.platform != "" {
		details["proxy_platform"] = v.platform
		summary += " · 网关/平台: " + v.platform
	}
	details["summary"] = summary
	if len(v.missing) > 0 {
		details["missing_fields"] = v.missing
	}

	if v.suspicious {
		attachIssues(details, []map[string]interface{}{newIssue(
			sevCritical, "backend_spoof_suspected",
			"疑似伪装 Anthropic：重写了 tool_id / 注入 service_tier，但缺失无法伪造的字段("+strings.Join(v.missing, ", ")+")",
		)})
		return DetectorResult{Status: "fail", Score: 30, Details: details}
	}
	// Anthropic direct, or a genuine alternate backend (Bedrock/Vertex) — both are
	// real Claude, so the detector passes and records the origin as evidence.
	return DetectorResult{Status: "pass", Score: 100, Details: details}
}

func boToolPayload(model string) map[string]interface{} {
	return map[string]interface{}{
		"model":      model,
		"max_tokens": 50,
		"tools": []interface{}{map[string]interface{}{
			"name":        "probe",
			"description": "Probe function",
			"input_schema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
				"required":   []string{"q"},
			},
		}},
		"tool_choice": map[string]interface{}{"type": "tool", "name": "probe"},
		"messages":    []map[string]interface{}{{"role": "user", "content": "call probe with q=test"}},
	}
}

// boThinkingPayload builds the thinking probe using the engine's adaptive-vs-
// extended shape so it does not 400 on adaptive-thinking models (opus-4.7/4.8,
// Claude 5).
func boThinkingPayload(model string) map[string]interface{} {
	body := map[string]interface{}{
		"model":      model,
		"max_tokens": 2048,
		"messages":   []map[string]interface{}{{"role": "user", "content": "What is 2+3?"}},
	}
	if info := lookupModel(model); info != nil && info.supportsExtendedThinking {
		body["thinking"] = map[string]interface{}{"type": "enabled", "budget_tokens": 1024}
	} else {
		body["thinking"] = map[string]interface{}{"type": "adaptive", "display": "summarized"}
		body["output_config"] = map[string]interface{}{"effort": adaptiveEffortForModel(model)}
	}
	return body
}
