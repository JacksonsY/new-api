package detector

// anthropic_behavior.go holds a battery of cheap behavioral probes ported from
// claude-detector (github.com/7836246/claude-detector, MIT, Copyright (c) 2026
// "Anthropic 妈妈测试" / anthropic.mom). Each catches a specific way a relay or
// mock diverges from genuine Anthropic behavior (stop-sequence semantics,
// max_tokens truncation, error envelope, prompt caching, multi-turn memory,
// response-header fingerprint). Attribution retained per the MIT license;
// additive, does not replace new-api / QuantumNous identity.

import (
	"context"
	"net/http"
	"strings"
)

// --- stop_sequence: a real model stops on the stop sequence and echoes it -----

func anthropicStopSequence(ctx context.Context, p *prober, cfg Config) DetectorResult {
	body := map[string]interface{}{
		"model":          cfg.Model,
		"max_tokens":     80,
		"stop_sequences": []string{"HALT"},
		"messages": []map[string]interface{}{
			{"role": "user", "content": `Say the words "one two three HALT and never more".`},
		},
	}
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}
	reason := strField(res.parsed, "stop_reason")
	matched := strField(res.parsed, "stop_sequence")
	passed := reason == "stop_sequence" && matched == "HALT"
	return simpleBehaviorResult(passed, map[string]interface{}{
		"stop_reason": reason, "stop_sequence": matched,
	})
}

// --- max_tokens: truncation must report stop_reason=max_tokens, output<=cap ----

func anthropicMaxTokensHonoring(ctx context.Context, p *prober, cfg Config) DetectorResult {
	body := map[string]interface{}{
		"model":      cfg.Model,
		"max_tokens": 5,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Write a very long essay about the history of computing, at least five hundred words."},
		},
	}
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}
	reason := strField(res.parsed, "stop_reason")
	out, _ := intField(anthropicUsage(res.parsed), "output_tokens")
	passed := reason == "max_tokens" && out > 0 && out <= 5
	return simpleBehaviorResult(passed, map[string]interface{}{
		"stop_reason": reason, "output_tokens": out,
	})
}

// --- error_shape: an invalid request must 4xx with the Anthropic error envelope

// isAnthropicErrorEnvelope reports whether a parsed body is a well-formed
// Anthropic error object {type:"error", error:{type:string, message:string}}.
func isAnthropicErrorEnvelope(parsed map[string]interface{}) bool {
	if parsed == nil || strField(parsed, "type") != "error" {
		return false
	}
	errObj := subMap(parsed, "error")
	if errObj == nil {
		return false
	}
	_, hasType := errObj["type"].(string)
	_, hasMsg := errObj["message"].(string)
	return hasType && hasMsg
}

func anthropicErrorShape(ctx context.Context, p *prober, cfg Config) DetectorResult {
	// Empty messages is an invalid request; a genuine endpoint 400s with the
	// error envelope. A relay that answers 200 (or a malformed error) fails.
	body := map[string]interface{}{"model": cfg.Model, "messages": []map[string]interface{}{}}
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if res.ok() {
		return DetectorResult{Status: "fail", Score: 0, Details: map[string]interface{}{
			"status_code": res.statusCode, "note": "非法请求却返回 200,非官方错误语义",
		}}
	}
	passed := isAnthropicErrorEnvelope(res.parsed)
	return simpleBehaviorResult(passed, map[string]interface{}{
		"status_code": res.statusCode, "error_type": strField(subMap(res.parsed, "error"), "type"),
	})
}

// --- cache_behavior: prompt caching must actually cache-read on the 2nd call ---

func anthropicCacheBehavior(ctx context.Context, p *prober, cfg Config) DetectorResult {
	sysText := strings.Repeat("You are a helpful assistant. This system prompt is intentionally padded to reach the minimum cache size. ", 15)
	body := map[string]interface{}{
		"model":      cfg.Model,
		"max_tokens": 8,
		"system": []interface{}{map[string]interface{}{
			"type": "text", "text": sysText,
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		}},
		"messages": []map[string]interface{}{{"role": "user", "content": "Say OK."}},
	}
	r1 := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
	if r1.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: r1.err.Error()}
	}
	if !r1.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(r1)}
	}
	cacheCreate, _ := intField(anthropicUsage(r1.parsed), "cache_creation_input_tokens")

	r2 := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
	if r2.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: r2.err.Error()}
	}
	if !r2.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(r2)}
	}
	cacheRead, _ := intField(anthropicUsage(r2.parsed), "cache_read_input_tokens")

	details := map[string]interface{}{"cache_creation": cacheCreate, "cache_read": cacheRead}
	switch {
	case cacheRead > 0:
		return DetectorResult{Status: "pass", Score: 100, Details: details}
	case cacheCreate > 0:
		// Cache was created but never read back — partial (some relays proxy the
		// create but not the read).
		return DetectorResult{Status: "fail", Score: 50, Details: details}
	default:
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	}
}

// --- multi_turn: earlier turns must survive (a mock/reverse-proxy may drop them)

func anthropicMultiTurn(ctx context.Context, p *prober, cfg Config) DetectorResult {
	body := map[string]interface{}{
		"model":      cfg.Model,
		"max_tokens": 20,
		"messages": []map[string]interface{}{
			{"role": "user", "content": `Remember this code: PINEAPPLE-7742. Just say "noted".`},
			{"role": "assistant", "content": "Noted."},
			{"role": "user", "content": "What was the code I asked you to remember? Reply with ONLY the code."},
		},
	}
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}
	text := strings.ToUpper(anthropicText(res.parsed))
	hasFruit := strings.Contains(text, "PINEAPPLE")
	hasNum := strings.Contains(text, "7742")
	score := 0.0
	if hasFruit {
		score += 50
	}
	if hasNum {
		score += 50
	}
	return DetectorResult{
		Status:  passFail(score, 100),
		Score:   score,
		Details: map[string]interface{}{"recalled_code": hasFruit && hasNum, "response_text": truncate(strings.TrimSpace(anthropicText(res.parsed)), 80)},
	}
}

// --- header_fingerprint: official infra returns request-id / cf-ray etc. -------

// scoreHeaderFingerprint counts how many of the 4 official-infra header markers
// are present (request-id, x-request-id, application/json content-type, cf-ray).
func scoreHeaderFingerprint(h http.Header) (hits int, present []string) {
	checks := []struct {
		name string
		ok   bool
	}{
		{"request-id", h.Get("request-id") != ""},
		{"x-request-id", h.Get("x-request-id") != ""},
		{"content-type=json", strings.Contains(h.Get("content-type"), "application/json")},
		{"cf-ray", h.Get("cf-ray") != ""},
	}
	for _, c := range checks {
		if c.ok {
			hits++
			present = append(present, c.name)
		}
	}
	return hits, present
}

func anthropicHeaderFingerprint(ctx context.Context, p *prober, cfg Config) DetectorResult {
	body := map[string]interface{}{
		"model": cfg.Model, "max_tokens": 4,
		"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
	}
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}
	hits, present := scoreHeaderFingerprint(res.header)
	score := float64(hits) / 4.0 * 100.0
	return DetectorResult{
		Status:  passFail(float64(hits), 2), // pass at >= 2/4
		Score:   score,
		Details: map[string]interface{}{"markers_present": present, "hits": hits},
	}
}

// simpleBehaviorResult maps a boolean pass to a 100/0 pass/fail result.
func simpleBehaviorResult(passed bool, details map[string]interface{}) DetectorResult {
	if passed {
		return DetectorResult{Status: "pass", Score: 100, Details: details}
	}
	return DetectorResult{Status: "fail", Score: 0, Details: details}
}
