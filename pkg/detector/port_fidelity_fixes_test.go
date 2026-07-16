package detector

// Regression tests for the Python→Go port-fidelity fixes. Each test pins a
// specific behavior that diverged from Veridrop and would silently misfire
// (temperature strip, count_tokens shape, Gemini URL join, OpenAI usage
// tolerance, long-context sizing/timeout, retry policy, key masking).

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A genuine Opus 4.7/4.8 upstream 400s on `temperature`; every probe body must
// have it stripped centrally (Veridrop client._sanitize_body), regardless of
// which detector built the payload.
func TestSanitizeProbeBodyStripsTemperatureForNewOpus(t *testing.T) {
	// Temperature-restricted models across providers: adaptive Opus 4.7/4.8 and
	// OpenAI gpt-5.5 (both 400 on temperature). Dotted and hyphen forms.
	strip := []string{
		"claude-opus-4-8", "claude-opus-4-7-20260101", "claude-opus-4.8",
		"gpt-5.5", "gpt-5-5", "gpt-5.5-mini",
	}
	for _, m := range strip {
		in := map[string]interface{}{"model": m, "temperature": 0, "max_tokens": 16}
		out, ok := sanitizeProbeBody(in).(map[string]interface{})
		require.True(t, ok)
		_, hasTemp := out["temperature"]
		assert.Falsef(t, hasTemp, "temperature must be stripped for %s", m)
		assert.Equal(t, 16, out["max_tokens"], "other fields must survive for %s", m)
		// The caller's map must not be mutated.
		_, origHasTemp := in["temperature"]
		assert.Truef(t, origHasTemp, "input map must not be mutated for %s", m)
	}

	// gpt-5.4 (not 5.5) and the other models keep temperature.
	keep := []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5", "gpt-5.4", "gpt-5.4-mini", "gemini-2.5-pro"}
	for _, m := range keep {
		in := map[string]interface{}{"model": m, "temperature": 0}
		out, ok := sanitizeProbeBody(in).(map[string]interface{})
		require.True(t, ok)
		_, hasTemp := out["temperature"]
		assert.Truef(t, hasTemp, "temperature must be preserved for %s", m)
	}

	// Non-map payloads pass through untouched.
	assert.Equal(t, "raw", sanitizeProbeBody("raw"))
}

// The Claude 5 family and gpt-5.6 (live on veridrop.org, past the local
// snapshot) must be fully wired: thinking_signature applies (not skipped),
// temperature stripped, adaptive xhigh effort, and correct long-context limit.
func TestNewModelWiring(t *testing.T) {
	for _, m := range []string{"claude-fable-5", "claude-sonnet-5", "claude-sonnet-5-20260101"} {
		assert.Truef(t, modelSupportsThinking(m), "%s: thinking_signature must apply (not skip)", m)
		assert.Truef(t, anthropicOmitsTemperature(m), "%s: temperature must be stripped", m)
		assert.Equalf(t, "xhigh", adaptiveEffortForModel(m), "%s: adaptive effort xhigh", m)
		assert.Equalf(t, 1_000_000, modelContextLimit(m), "%s: long-context limit 1M", m)
	}
	// gpt-5.6 temperature-fixed like gpt-5.5; gpt-5.4 keeps temperature.
	assert.True(t, openaiOmitsTemperature("gpt-5.6"))
	assert.True(t, openaiOmitsTemperature("gpt-5.6-mini"))
	assert.False(t, openaiOmitsTemperature("gpt-5.4"))
	// gpt-5.6 long-context resolves via the gpt-5 prefix (272k).
	assert.Equal(t, 272_000, modelContextLimit("gpt-5.6"))
}

// The Anthropic protocol validator counts DISTINCT invalid values of the four
// value-suffixed issue kinds separately (a swapped backend forwarding
// stop/length as stop_reason should be penalized per value, not once).
func TestAnthropicProtocolPerValueDedup(t *testing.T) {
	mk := func(stopReason string) map[string]interface{} {
		return map[string]interface{}{
			"id": "msg_x", "type": "message", "role": "assistant", "model": "claude-x",
			"content":     []interface{}{},
			"stop_reason": stopReason,
			"usage":       map[string]interface{}{"input_tokens": float64(1), "output_tokens": float64(1)},
		}
	}
	p := &prober{tel: &runTelemetry{}}
	p.tel.observations = []observation{
		{response: mk("stop")},   // invalid (OpenAI-style)
		{response: mk("length")}, // a second, distinct invalid value
	}
	res := anthropicProtocol(context.Background(), p, Config{})
	issues, _ := res.Details["issues"].([]string)
	n := 0
	for _, is := range issues {
		if strings.HasPrefix(is, "stop_reason_invalid:") {
			n++
		}
	}
	assert.Equal(t, 2, n, "distinct invalid stop_reasons must count separately")
}

// OpenAI integrity uses the reasoning-tolerant comparator, NOT Gemini's
// ≥2-of-3-within-1 one: a lower stream completion (hidden reasoning tokens) is
// compatible, total_tokens is ignored, and over-reporting is rejected.
func TestUsageCloseOpenAIReasoningTolerant(t *testing.T) {
	// Lower stream completion + wildly different total → OpenAI accepts, the
	// Gemini comparator rejects (this is the exact bug the fix corrects).
	nonStream := map[string]interface{}{"prompt_tokens": 100.0, "completion_tokens": 100.0, "total_tokens": 200.0}
	stream := map[string]interface{}{"prompt_tokens": 100.0, "completion_tokens": 60.0, "total_tokens": 160.0}
	assert.True(t, usageCloseOpenAI(nonStream, stream), "reasoning-lower completion must be compatible")
	assert.False(t, usageCloseAll(nonStream, stream, 1), "gemini comparator would reject the same input")

	cases := []struct {
		name      string
		non, strm map[string]interface{}
		wantClose bool
	}{
		{"prompt off by 2 rejected",
			map[string]interface{}{"prompt_tokens": 100.0, "completion_tokens": 100.0},
			map[string]interface{}{"prompt_tokens": 102.0, "completion_tokens": 100.0}, false},
		{"over-reporting rejected",
			map[string]interface{}{"prompt_tokens": 100.0, "completion_tokens": 100.0},
			map[string]interface{}{"prompt_tokens": 100.0, "completion_tokens": 200.0}, false},
		{"zero stream completion rejected",
			map[string]interface{}{"prompt_tokens": 100.0, "completion_tokens": 100.0},
			map[string]interface{}{"prompt_tokens": 100.0, "completion_tokens": 0.0}, false},
		{"equal usage accepted",
			map[string]interface{}{"prompt_tokens": 50.0, "completion_tokens": 20.0},
			map[string]interface{}{"prompt_tokens": 50.0, "completion_tokens": 20.0}, true},
		{"empty rejected", map[string]interface{}{}, map[string]interface{}{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.wantClose, usageCloseOpenAI(c.non, c.strm))
		})
	}
}

// OpenAI/Grok resolve to .../v1/chat/completions; Anthropic to /v1/messages;
// Gemini now speaks NATIVE generateContent, so its path is built from the model
// name with /v1beta de-dup for a base a user pasted with an explicit /v1beta.
func TestBuildURLProtocolAware(t *testing.T) {
	cases := []struct {
		protocol, base, want string
	}{
		{ProtocolOpenAI, "https://host", "https://host/v1/chat/completions"},
		{ProtocolOpenAI, "https://host/v1", "https://host/v1/chat/completions"},
		{ProtocolOpenAI, "https://host/custom", "https://host/custom/v1/chat/completions"},
		{ProtocolGrok, "https://host", "https://host/v1/chat/completions"},
	}
	for _, c := range cases {
		p := &prober{baseURL: c.base, protocol: c.protocol}
		assert.Equalf(t, c.want, p.buildURL(openaiChatPath), "%s %s", c.protocol, c.base)
	}
	// Anthropic Messages keeps the /v1 de-dup behavior.
	pa := &prober{baseURL: "https://host/v1", protocol: ProtocolAnthropic}
	assert.Equal(t, "https://host/v1/messages", pa.buildURL(anthropicMessagesPath))

	// Native Gemini generateContent path, with /v1beta de-dup.
	pg := &prober{baseURL: "https://host", protocol: ProtocolGemini}
	assert.Equal(t, "https://host/v1beta/models/gemini-2.5-flash:generateContent",
		pg.buildURL(geminiNativePath("gemini-2.5-flash", false)))
	pgv := &prober{baseURL: "https://host/v1beta", protocol: ProtocolGemini}
	assert.Equal(t, "https://host/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse",
		pgv.buildURL(geminiNativePath("gemini-2.5-flash", true)))
}

// An oversized haystack that trips Anthropic's real context ceiling is a
// detector sizing issue (skip), not relay truncation (fail) — but only when the
// reported maximum equals the model's advertised limit.
func TestLooksPromptOverflow(t *testing.T) {
	assert.True(t, looksPromptOverflow("prompt is too long: 250000 tokens > 200000 maximum", 200_000))
	assert.True(t, looksPromptOverflow("prompt is too long: 1,050,000 tokens > 1,000,000 maximum", 1_000_000))
	// max != ctxLimit: a genuinely truncating relay reporting a smaller ceiling.
	assert.False(t, looksPromptOverflow("prompt is too long: 250000 tokens > 200000 maximum", 1_000_000))
	assert.False(t, looksPromptOverflow("rate_limit_exceeded", 200_000))
	assert.False(t, looksPromptOverflow("", 200_000))
}

// Long-context tiers need a per-request timeout that scales with input size, far
// beyond the default probe timeout (a 30s cap false-fails every big tier).
func TestLcTierTimeout(t *testing.T) {
	assert.Equal(t, 120*time.Second, lcTierTimeout(32_000))  // floor
	assert.Equal(t, 120*time.Second, lcTierTimeout(100_000)) // still floor
	assert.Equal(t, 250*time.Second, lcTierTimeout(1_000_000))
	assert.Equal(t, 237*time.Second, lcTierTimeout(950_000))
}

// Retry policy is per-protocol: Anthropic retries 4× and treats 529 as
// retryable; OpenAI/Gemini retry 3× and do not.
func TestRetryPolicyPerProtocol(t *testing.T) {
	assert.Equal(t, 5, maxAttempts(ProtocolAnthropic))
	assert.Equal(t, 4, maxAttempts(ProtocolOpenAI))
	assert.Equal(t, 4, maxAttempts(ProtocolGemini))

	assert.True(t, retryableStatus(ProtocolAnthropic, 529))
	assert.False(t, retryableStatus(ProtocolOpenAI, 529))
	assert.False(t, retryableStatus(ProtocolGemini, 529))
	for _, code := range []int{429, 500, 502, 503, 504} {
		assert.Truef(t, retryableStatus(ProtocolOpenAI, code), "status %d", code)
	}
	for _, code := range []int{200, 400, 404, 401} {
		assert.Falsef(t, retryableStatus(ProtocolAnthropic, code), "status %d", code)
	}
}

// Backoff is min(2^(attempt-1), 30s) with a fractional-seconds Retry-After honored
// and capped.
func TestBackoffDelay(t *testing.T) {
	assert.Equal(t, 1*time.Second, backoffDelay(1, nil))
	assert.Equal(t, 2*time.Second, backoffDelay(2, nil))
	assert.Equal(t, 16*time.Second, backoffDelay(5, nil))
	assert.Equal(t, 30*time.Second, backoffDelay(9, nil)) // capped

	frac := http.Header{"Retry-After": []string{"2.5"}}
	assert.Equal(t, 2500*time.Millisecond, backoffDelay(1, frac))
	big := http.Header{"Retry-After": []string{"100"}}
	assert.Equal(t, 30*time.Second, backoffDelay(1, big)) // capped
}

// api_key_masked matches Veridrop mask_api_key exactly.
func TestMaskAPIKeyFormat(t *testing.T) {
	assert.Equal(t, "sk-abc••••••kl", maskAPIKey("sk-abcdefghijkl"))
	assert.Equal(t, "ab••", maskAPIKey("abcd"))
	assert.Equal(t, "ab", maskAPIKey("ab"))
	assert.Equal(t, "", maskAPIKey(""))
	assert.Equal(t, "", maskAPIKey("   "))
}

// A successful stream is reconstructed into a non-stream Messages envelope on the
// passive-observation bus so protocol/message_id validate streamed traffic too.
func TestRecordAnthropicStreamObservation(t *testing.T) {
	out := 5
	in := 10
	// The synthesized envelope starts from the real message_start.message, so a
	// stream-only wrong type/role IS carried through to the passive validators.
	s := anthropicStream{
		id:    "msg_abc",
		model: "claude-opus-4-8",
		messageStart: map[string]interface{}{
			"type": "message", "role": "assistant", "id": "msg_abc", "model": "claude-opus-4-8",
			"content": []interface{}{}, "usage": map[string]interface{}{"input_tokens": float64(10)},
		},
		stopReason: "end_turn", startInput: &in, deltaOutput: &out, chunkCount: 3,
	}

	p := &prober{tel: &runTelemetry{}}
	p.recordAnthropicStreamObservation(s, httpResult{statusCode: 200})
	obs := p.tel.snapshot()
	require.Len(t, obs, 1)
	env := obs[0].response
	assert.Equal(t, "message", env["type"])
	assert.Equal(t, "assistant", env["role"], "role must be carried from message_start, not hardcoded")
	assert.Equal(t, "msg_abc", env["id"])
	assert.Equal(t, "claude-opus-4-8", env["model"])
	assert.Equal(t, "end_turn", env["stop_reason"])
	usage, _ := env["usage"].(map[string]interface{})
	require.NotNil(t, usage)
	// Numbers are normalized to float64 (JSON round-trip) so passive validators
	// (isNonNegInt/intField) see the same shape as a real parsed response.
	assert.EqualValues(t, 10, usage["input_tokens"])
	assert.IsType(t, float64(0), usage["input_tokens"])
	assert.EqualValues(t, 5, usage["output_tokens"])

	// A failed stream (non-2xx), an empty stream, or a stream with no
	// message_start records nothing.
	pFail := &prober{tel: &runTelemetry{}}
	pFail.recordAnthropicStreamObservation(s, httpResult{statusCode: 500})
	assert.Empty(t, pFail.tel.snapshot())
	pEmpty := &prober{tel: &runTelemetry{}}
	pEmpty.recordAnthropicStreamObservation(anthropicStream{chunkCount: 2}, httpResult{statusCode: 200})
	assert.Empty(t, pEmpty.tel.snapshot())
}

// Detection egresses through the caller-injected transport (the global proxy);
// when none is injected the prober builds its own client. The up-front SSRF
// floor (guardBaseURL) holds in both cases.
func TestNewProberUsesInjectedTransport(t *testing.T) {
	sentinel := &http.Transport{}
	p, err := newProber(Config{BaseURL: "https://8.8.8.8", TimeoutSeconds: 5, Protocol: ProtocolOpenAI, Transport: sentinel})
	require.NoError(t, err)
	assert.Same(t, sentinel, p.client.Transport, "injected proxy transport must be used verbatim")

	p2, err := newProber(Config{BaseURL: "https://8.8.8.8", TimeoutSeconds: 5, Protocol: ProtocolOpenAI})
	require.NoError(t, err)
	require.NotNil(t, p2.client.Transport)
	assert.NotSame(t, sentinel, p2.client.Transport, "no transport injected → prober builds its own")

	// SSRF floor still rejects a blocked target even when a proxy transport is
	// supplied (guardBaseURL runs before transport selection).
	_, err = newProber(Config{BaseURL: "http://127.0.0.1", TimeoutSeconds: 5, Transport: sentinel})
	require.Error(t, err)
}

// The token_billing completion bound is applied to user-visible output, not raw
// completion_tokens, so a reasoning model (gpt-5.x: 17 = 10 reasoning + 7 visible)
// is not falsely flagged — while genuine inflation is still caught.
func TestVisibleCompletionExcludesReasoning(t *testing.T) {
	reasoning := map[string]interface{}{
		"completion_tokens":         float64(17),
		"completion_tokens_details": map[string]interface{}{"reasoning_tokens": float64(10)},
	}
	v, ok := visibleCompletion(reasoning)
	require.True(t, ok)
	assert.Equal(t, 7, v)
	assert.True(t, completionSaneBounded(reasoning, 1, obCompletionMaxOK), "visible 7 must pass the ≤12 bound")
	assert.Greater(t, 17, obCompletionMaxOK, "raw 17 would have failed")

	// No reasoning details → raw completion used unchanged.
	plain := map[string]interface{}{"completion_tokens": float64(9)}
	v2, ok2 := visibleCompletion(plain)
	require.True(t, ok2)
	assert.Equal(t, 9, v2)

	// Real inflation (no reasoning) is still rejected.
	inflated := map[string]interface{}{"completion_tokens": float64(40)}
	assert.False(t, completionSaneBounded(inflated, 1, obCompletionMaxOK))
}

// A well-formed synthesized stream envelope (genuine relay: chatcmpl- id,
// created, valid choice) must produce ZERO protocol issues. The #8 regression
// omitted `created` and used a Go-int index, manufacturing false criticals/majors
// on the passive protocol validator — this pins that it no longer does.
func TestSyntheticStreamEnvelopePassesProtocol(t *testing.T) {
	p := &prober{tel: &runTelemetry{}, protocol: ProtocolGemini}
	s := openaiStream{
		id:           "chatcmpl-abc",
		model:        "gemini-2.5-pro",
		created:      float64(1_700_000_000),
		text:         "pong",
		finishReason: "stop",
		chunkCount:   3,
		usage: map[string]interface{}{
			"prompt_tokens": float64(10), "completion_tokens": float64(2), "total_tokens": float64(12),
		},
	}
	p.recordChatStreamObservation(s, httpResult{statusCode: 200})
	obs := p.tel.snapshot()
	require.Len(t, obs, 1)

	score, issues := validateChatCompletion(obs[0].response, "gemini-2.5-pro")
	var offending []string
	for _, is := range issues {
		offending = append(offending, is.severity+":"+is.code)
	}
	assert.Empty(t, offending, "synthetic envelope must not manufacture protocol issues")
	assert.Equal(t, 100.0, score)
}

func TestRecordChatStreamObservation(t *testing.T) {
	s := openaiStream{id: "chatcmpl-x", model: "gpt-5.4", text: "pong", finishReason: "stop", usage: map[string]interface{}{"prompt_tokens": 5.0}, chunkCount: 2}

	p := &prober{tel: &runTelemetry{}}
	p.recordChatStreamObservation(s, httpResult{statusCode: 200})
	obs := p.tel.snapshot()
	require.Len(t, obs, 1)
	env := obs[0].response
	assert.Equal(t, "chat.completion", env["object"])
	assert.Equal(t, "chatcmpl-x", env["id"])
	assert.Equal(t, "gpt-5.4", env["model"])
	choices, _ := env["choices"].([]interface{})
	require.Len(t, choices, 1)
	c0, _ := choices[0].(map[string]interface{})
	assert.Equal(t, "stop", c0["finish_reason"])

	pFail := &prober{tel: &runTelemetry{}}
	pFail.recordChatStreamObservation(s, httpResult{statusCode: 429})
	assert.Empty(t, pFail.tel.snapshot())
}

// A detector that errored only because the overall run budget fired is a
// casualty of the deadline, not evidence about the model: runOne reclassifies it
// to skip so computeTotal excludes it, instead of averaging a full-weight 0 that
// would drag a genuine relay's verdict down. A genuine error while the budget is
// still alive keeps its error status and its 0.
func TestRunOneDeadlineCasualtyBecomesSkip(t *testing.T) {
	errFn := func(_ context.Context, _ *prober, _ Config) DetectorResult {
		return DetectorResult{Status: "error", Score: 0, Error: "context deadline exceeded"}
	}
	d := detectorDef{name: "x", displayName: "X", weight: 5, fn: errFn}

	// Live context: a real error is real evidence — keep it (scored 0).
	live := runOne(context.Background(), nil, Config{}, d)
	assert.Equal(t, "error", live.Status)

	// Overall budget fired (ctx cancelled): the error is a casualty → skip, with
	// the detector identity/weight still stamped.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	casualty := runOne(ctx, nil, Config{}, d)
	assert.Equal(t, "skip", casualty.Status)
	assert.Equal(t, "x", casualty.Name)
	assert.Equal(t, 5.0, casualty.Weight)

	// computeTotal excludes the skip, so a lone genuine pass is not diluted by a
	// full-weight 0 (the exact drag this fix removes).
	total := computeTotal([]DetectorResult{
		{Status: "pass", Score: 100, Weight: 10},
		casualty,
	})
	assert.Equal(t, 100.0, total)
}
