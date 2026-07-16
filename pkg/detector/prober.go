package detector

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// maxResponseBytes caps how much of a probe response we read. Probes use tiny
// prompts, so this is only a guard against a hostile/huge upstream body.
const maxResponseBytes = 1 << 20 // 1 MiB

// probe retry policy (mirrors Veridrop's per-protocol clients: Anthropic retries
// 4× and treats 529 overloaded_error as retryable; OpenAI/Gemini retry 3× and do
// not retry 529. Backoff is min(2^attempt, 30s); Retry-After — fractional seconds
// accepted — is capped the same way).
const (
	probeBackoffMax    = 30 * time.Second
	probeRetryAfterCap = 30 * time.Second
)

// maxAttempts returns the total attempt count (initial try + retries) for a
// protocol, matching Veridrop's MAX_RETRIES + 1 (Anthropic 4, OpenAI/Gemini 3).
func maxAttempts(protocol string) int {
	if protocol == ProtocolAnthropic {
		return 5
	}
	return 4
}

// observation is one non-stream (request→response) pair captured during a run.
// Passive detectors (protocol, message_id) validate every observation in the
// run instead of issuing their own probes, mirroring Veridrop's PassiveDetector
// broadcast bus.
type observation struct {
	response   map[string]interface{}
	statusCode int
	latencyMs  int64
}

// runTelemetry accumulates the passive-observation bus plus performance
// counters (request count, latency, TTFT proxy, backoff events) across a run.
// All probes share one instance; access is mutex-guarded because active
// detectors run concurrently.
type runTelemetry struct {
	mu             sync.Mutex
	observations   []observation
	requestCount   int
	totalLatencyMs int64
	firstLatencyMs int64
	backoffEvents  int
}

func (t *runTelemetry) record(res httpResult, observe bool, backoffs, attempts int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Count every HTTP attempt (Veridrop increments request_count per attempt
	// inside the retry loop, not once per logical probe).
	if attempts < 1 {
		attempts = 1
	}
	t.requestCount += attempts
	t.backoffEvents += backoffs
	t.totalLatencyMs += res.durationMs
	if t.firstLatencyMs == 0 && res.durationMs > 0 {
		t.firstLatencyMs = res.durationMs
	}
	if observe && res.ok() && res.parsed != nil {
		t.observations = append(t.observations, observation{
			response:   res.parsed,
			statusCode: res.statusCode,
			latencyMs:  res.durationMs,
		})
	}
}

func (t *runTelemetry) snapshot() []observation {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]observation, len(t.observations))
	copy(out, t.observations)
	return out
}

// performance summarizes the run's telemetry into the report block, aggregating
// token usage across observed responses. Returns nil when nothing was sent.
func (p *prober) performance() *PerformanceMetrics {
	if p.tel == nil {
		return nil
	}
	p.tel.mu.Lock()
	defer p.tel.mu.Unlock()
	if p.tel.requestCount == 0 {
		return nil
	}
	var inTok, outTok int
	for _, o := range p.tel.observations {
		u, ok := o.response["usage"].(map[string]interface{})
		if !ok {
			continue
		}
		// Anthropic reports input_tokens/output_tokens; OpenAI & Gemini report
		// prompt_tokens/completion_tokens (Veridrop _absorb_response_usage maps
		// both onto the same aggregate). Prefer the native key, fall back to the
		// OpenAI-style one so all three protocols populate the perf block.
		if v, ok := intField(u, "input_tokens"); ok {
			inTok += v
		} else if v, ok := intField(u, "prompt_tokens"); ok {
			inTok += v
		}
		if v, ok := intField(u, "output_tokens"); ok {
			outTok += v
		} else if v, ok := intField(u, "completion_tokens"); ok {
			outTok += v
		}
	}
	tps := 0.0
	if p.tel.totalLatencyMs > 0 {
		tps = float64(outTok) / (float64(p.tel.totalLatencyMs) / 1000.0)
	}
	return &PerformanceMetrics{
		RequestCount:    p.tel.requestCount,
		TotalLatencyMs:  p.tel.totalLatencyMs,
		TtftMs:          p.tel.firstLatencyMs,
		InputTokens:     inTok,
		OutputTokens:    outTok,
		TokensPerSecond: math.Round(tps*100) / 100,
		BackoffEvents:   p.tel.backoffEvents,
	}
}

// retryableStatus reports whether an HTTP status warrants a probe retry for the
// given protocol. 529 (overloaded_error) is retryable only for Anthropic, per
// Veridrop's per-protocol RETRYABLE_STATUS sets.
func retryableStatus(protocol string, code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	case 529: // Anthropic overloaded_error
		return protocol == ProtocolAnthropic
	}
	return false
}

// prober performs the outbound HTTP probes against the target relay. It is
// constructed with an SSRF-guarded HTTP client that never follows redirects.
type prober struct {
	client   *http.Client
	baseURL  string
	apiKey   string
	protocol string
	timeout  time.Duration // default per-request timeout
	tel      *runTelemetry
	// openaiSurface is the preferred OpenAI/Grok chat surface for the shared
	// probeChat dispatch, resolved once by resolveOpenAISurface. "" until
	// resolved (probeChat then defaults to chat); irrelevant for anthropic/gemini.
	openaiSurface string
}

// OpenAI/Grok chat surfaces the shared probes can dispatch to.
const (
	surfaceChat      = "chat"      // POST /v1/chat/completions
	surfaceResponses = "responses" // POST /v1/responses (preferred when served)
)

// resolveOpenAISurface probes /v1/responses once so the shared chat probes
// PREFER the Responses API — the modern native surface for gpt-5.x / o-series
// and xAI's current default (chat/completions is now marked legacy) — when the
// relay serves it, falling back to Chat Completions otherwise. No-op for
// anthropic/gemini. Call it once before the concurrent detector phase so
// probeChat sees a stable surface (the prober is shared across goroutines).
func (p *prober) resolveOpenAISurface(ctx context.Context, model string) {
	if p.protocol != ProtocolOpenAI && p.protocol != ProtocolGrok {
		return
	}
	p.openaiSurface = surfaceChat
	// Run the capability probe on a DETACHED telemetry so this internal check
	// does not become the run's first recorded request — otherwise TtftMs would
	// measure the tiny "ping" (or a fast 404) instead of a real detection probe,
	// and requestCount would include a request the performance block doesn't
	// describe. The struct copy is safe: resolve runs before the concurrent phase.
	probe := *p
	probe.tel = &runTelemetry{}
	res := probe.postJSONUnobserved(ctx, responsesPath, openaiHeaders(p.apiKey), responsesBody(model, "ping", 16))
	if res.ok() && (strings.HasPrefix(responsesID(res.parsed), "resp_") || strField(res.parsed, "object") == "response") {
		p.openaiSurface = surfaceResponses
	}
}

// httpResult is the outcome of a single probe request.
type httpResult struct {
	statusCode int
	header     http.Header
	body       []byte
	text       string
	parsed     map[string]interface{}
	durationMs int64
	err        error
}

// ok reports a successful 2xx JSON-or-text response with no transport error.
func (r httpResult) ok() bool {
	return r.err == nil && r.statusCode >= 200 && r.statusCode < 300
}

// newProber validates the base URL against the SSRF guard and builds an HTTP
// client whose dialer re-checks every resolved IP at connect time (defense
// against DNS rebinding) and which refuses to follow redirects.
func newProber(cfg Config) (*prober, error) {
	if err := guardBaseURL(cfg.BaseURL); err != nil {
		return nil, err
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second

	// Egress through the caller-provided transport (the fork's global outbound
	// proxy) when set; otherwise a direct connection guarded at dial time. With a
	// proxy, DNS/egress happen at the proxy so the dial-time IP guard cannot cover
	// the target — guardBaseURL (up front) is the SSRF floor, and probes are POST
	// (non-idempotent) so the proxy's direct-fallback never fires for them.
	transport := cfg.Transport
	if transport == nil {
		// Connection establishment stays bounded here; the overall per-request
		// budget is enforced by a context deadline in doOnce so a single long
		// probe (e.g. a long-context tier) can extend beyond the default without
		// lifting the connect/handshake caps.
		connectTimeout := timeout
		if connectTimeout > 30*time.Second {
			connectTimeout = 30 * time.Second
		}
		dialer := &net.Dialer{
			Timeout:   connectTimeout,
			KeepAlive: 30 * time.Second,
			// Control runs after DNS resolution with the concrete IP:port about to
			// be dialed, so it blocks DNS-rebinding into private ranges.
			Control: func(_, address string, _ syscall.RawConn) error {
				return guardDialAddress(address)
			},
		}
		transport = &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: connectTimeout,
			// No ResponseHeaderTimeout: a long-context tier can take minutes to
			// return headers. The per-request context deadline (doOnce) bounds the
			// whole exchange instead, so it can scale per probe.
			ExpectContinueTimeout: time.Second,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			ForceAttemptHTTP2:     true,
		}
	}
	client := &http.Client{
		Transport: transport,
		// No client-level Timeout: every request carries its own context
		// deadline (default p.timeout, overridden per long-context tier).
		// Never follow redirects: a 30x could point into a private range, and
		// following it would bypass the up-front host check.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &prober{
		client:   client,
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:   cfg.APIKey,
		protocol: cfg.Protocol,
		timeout:  timeout,
		tel:      &runTelemetry{},
	}, nil
}

// guardBaseURL validates a user-supplied base URL before any request is made:
// it must be http(s), have a host, and must not resolve to a loopback,
// private, link-local (incl. 169.254.169.254 cloud metadata), unique-local
// IPv6, or unspecified address. Resolution failures are treated as fatal
// (fail closed).
//
// This up-front check is the SSRF floor for every probe. It is a hard-coded
// guard rather than the repo's admin-configurable ValidateURLWithFetchSetting
// because the public /api/detector endpoint must probe ARBITRARY user-supplied
// relays: an operator allow-list (whitelist mode) would break legitimate
// detection, and honoring AllowPrivateIp would open the public path to internal
// SSRF. Actual egress goes through the fork's global outbound proxy when one is
// configured (Config.Transport); when it is not, guardDialAddress re-validates
// the resolved IP at dial time (DNS-rebind defense). The floor can never be
// loosened by configuration.
func guardBaseURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("base_url is empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported base_url scheme %q (only http/https allowed)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("base_url has no host")
	}

	// IP literal: check directly, no DNS.
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("base_url address %s is not allowed", ip)
		}
		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve base_url host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("base_url host %q resolved to no addresses", host)
	}
	// Reject if ANY resolved address is blocked (an attacker could return a
	// mix of public and private records).
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("base_url host %q resolves to blocked address %s", host, ip)
		}
	}
	return nil
}

// guardDialAddress validates the concrete IP:port the dialer is about to
// connect to.
func guardDialAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("refusing to dial non-IP address %q", address)
	}
	if isBlockedIP(ip) {
		return fmt.Errorf("refusing to dial blocked address %s", ip)
	}
	return nil
}

// nat64WellKnownPrefix is the NAT64 well-known prefix (RFC6052). Addresses in it
// embed an IPv4 in the low 32 bits, so a probe could reach an internal/metadata
// IPv4 through it; block the whole prefix.
var nat64WellKnownPrefix = netip.MustParsePrefix("64:ff9b::/96")

// isBlockedIP reports whether an IP is in a range that must never be reached
// from a server-side probe of user-supplied input.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	// IsPrivate covers RFC1918 (10/8, 172.16/12, 192.168/16) and IPv6
	// unique-local fc00::/7.
	if addr.IsPrivate() {
		return true
	}
	if addr.IsLoopback() || addr.IsUnspecified() || addr.IsLinkLocalUnicast() {
		return true
	}
	// CGNAT 100.64.0.0/10 (RFC6598): not covered by IsPrivate, and hosts cloud
	// metadata such as Alibaba Cloud's 100.100.100.200 — must not be reachable.
	if addr.Is4() {
		b := addr.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return true
		}
	}
	if nat64WellKnownPrefix.Contains(addr) {
		return true
	}
	return false
}

// buildURL joins the prober's base URL with a path suffix. The Chat Completions
// suffix follows Veridrop's per-protocol normalization: OpenAI always resolves
// to ".../v1/chat/completions" (normalize_openai_base_url appends /v1 unless
// present), while Gemini preserves an explicit path such as "/v1beta/openai" or
// "/v1" and only injects "/v1" for a bare host root (normalize_gemini_base_url).
// Anthropic Messages / count_tokens keep the "/v1" de-dup behavior.
func (p *prober) buildURL(suffix string) string {
	base := strings.TrimRight(p.baseURL, "/")
	if suffix == openaiChatPath {
		if p.protocol == ProtocolGemini {
			if isBareHostRoot(base) {
				return base + openaiChatPath
			}
			return base + "/chat/completions"
		}
		if strings.HasSuffix(base, "/v1") {
			return base + "/chat/completions"
		}
		return base + openaiChatPath
	}
	// Native Gemini generateContent path: append under the base, de-duping an
	// explicit ".../v1beta" the user may have pasted (Google's own base is the
	// bare host, a relay's is its root — both take the full /v1beta/models/… ).
	if strings.HasPrefix(suffix, "/v1beta/") {
		if strings.HasSuffix(base, "/v1beta") {
			return base + strings.TrimPrefix(suffix, "/v1beta")
		}
		return base + suffix
	}
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(suffix, "/v1/") {
		return base + strings.TrimPrefix(suffix, "/v1")
	}
	return base + suffix
}

// isBareHostRoot reports whether a base URL is just scheme://host with no path
// (or "/") — the only shape for which the Gemini normalizer injects "/v1".
func isBareHostRoot(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != "" && (u.Path == "" || u.Path == "/")
}

// sanitizeProbeBody mirrors Veridrop's client._sanitize_body: it strips request
// fields the target model is known to reject — temperature on adaptive Opus
// 4.7/4.8 (anthropic) and on gpt-5.5 (openai) — keyed on the body's model so
// detector payloads stay model-agnostic and no probe 400s against a genuine
// upstream. Returns the input unchanged unless a strip is needed, in which case
// it returns a shallow copy so the caller's map is never mutated.
func sanitizeProbeBody(payload any) any {
	body, ok := payload.(map[string]interface{})
	if !ok {
		return payload
	}
	model, _ := body["model"].(string)
	if !anthropicOmitsTemperature(model) && !openaiOmitsTemperature(model) {
		return payload
	}
	if _, has := body["temperature"]; !has {
		return payload
	}
	clone := make(map[string]interface{}, len(body))
	for k, v := range body {
		if k == "temperature" {
			continue
		}
		clone[k] = v
	}
	return clone
}

// postJSON sends a JSON POST (with retry/backoff on transient 429/5xx/529 and
// transport errors), reads the capped body, parses JSON objects, and records the
// response on the passive-observation bus. Transport errors are returned in
// httpResult.err; the caller decides how to score them.
func (p *prober) postJSON(ctx context.Context, path string, headers map[string]string, payload any) httpResult {
	return p.doWithTimeout(ctx, path, headers, payload, true, p.timeout, maxAttempts(p.protocol))
}

// postJSONUnobserved is postJSON that does NOT record the response on the passive
// observation bus. Use it for probes whose response shape differs from the
// protocol's chat/messages envelope — the Responses API (object=response,
// output[]) and count_tokens — so they don't trip the passive protocol validator
// that expects the primary surface's shape.
func (p *prober) postJSONUnobserved(ctx context.Context, path string, headers map[string]string, payload any) httpResult {
	return p.doWithTimeout(ctx, path, headers, payload, false, p.timeout, maxAttempts(p.protocol))
}

// postJSONTimeout is postJSON with an explicit per-request timeout, used by the
// long-context detector whose large tiers need minutes, not the default seconds.
func (p *prober) postJSONTimeout(ctx context.Context, path string, headers map[string]string, payload any, timeout time.Duration) httpResult {
	return p.doWithTimeout(ctx, path, headers, payload, true, timeout, maxAttempts(p.protocol))
}

// postSSE sends a streaming (stream:true) JSON POST and returns the raw
// event-stream body as text. The streamed response is not recorded here; the
// per-protocol stream probes synthesize a non-stream-equivalent envelope and
// call recordStreamObservation so passive detectors validate streamed traffic
// too (Veridrop's client broadcasts synthesized stream responses).
func (p *prober) postSSE(ctx context.Context, path string, headers map[string]string, payload any) httpResult {
	sseHeaders := map[string]string{"Accept": "text/event-stream"}
	for k, v := range headers {
		sseHeaders[k] = v
	}
	// Stream probes are single-attempt (no HTTP retry): the reference clients
	// route only non-stream/count_tokens through _with_retry, never streams, so a
	// transient 429/5xx on a stream fails soft (scored from the non-stream side /
	// anthropic integrity errors) rather than being silently recovered.
	return p.doWithTimeout(ctx, path, sseHeaders, payload, false, p.timeout, 1)
}

// streamTimed sends a streaming POST and reads the event stream incrementally to
// measure time-to-first-token (the wall-clock ms to the first SSE data chunk for
// which firstToken(obj) is true — i.e. the first visible content delta) and the
// total time, returning the full raw body too. Single attempt (no retry), like
// postSSE. Unlike postJSON/postSSE (which io.ReadAll the whole body before any
// timing is possible), this is the only path that can observe TTFT.
func (p *prober) streamTimed(ctx context.Context, path string, headers map[string]string, payload any, firstToken func(map[string]interface{}) bool, reqTimeout time.Duration) (ttftMs, totalMs int64, body string, err error) {
	payload = sanitizeProbeBody(payload)
	bodyBytes, merr := common.Marshal(payload)
	if merr != nil {
		return 0, 0, "", fmt.Errorf("marshal payload: %w", merr)
	}
	reqCtx := ctx
	if reqTimeout > 0 {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, reqTimeout)
		defer cancel()
	}
	req, rerr := http.NewRequestWithContext(reqCtx, http.MethodPost, p.buildURL(path), bytes.NewReader(bodyBytes))
	if rerr != nil {
		return 0, 0, "", fmt.Errorf("build request: %w", rerr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, derr := p.client.Do(req)
	if derr != nil {
		return 0, time.Since(start).Milliseconds(), "", derr
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return 0, time.Since(start).Milliseconds(), string(data), fmt.Errorf("stream HTTP %d", resp.StatusCode)
	}

	var sb strings.Builder
	var read int64
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		read += int64(len(line)) + 1
		if read > maxResponseBytes {
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
		if ttftMs == 0 {
			trimmed := strings.TrimSpace(line)
			if payloadStr, ok := strings.CutPrefix(trimmed, "data:"); ok {
				payloadStr = strings.TrimSpace(payloadStr)
				if payloadStr != "" && payloadStr != "[DONE]" {
					var obj map[string]interface{}
					if common.UnmarshalJsonStr(payloadStr, &obj) == nil && firstToken(obj) {
						ttftMs = time.Since(start).Milliseconds()
					}
				}
			}
		}
	}
	totalMs = time.Since(start).Milliseconds()
	if ttftMs == 0 {
		ttftMs = totalMs // never saw a visible content token; fall back to total
	}
	return ttftMs, totalMs, sb.String(), nil
}

// recordStreamObservation places a synthesized non-stream-equivalent response on
// the passive-observation bus, mirroring Veridrop clients that broadcast a dict
// reconstructed from stream events so protocol/message_id see streamed traffic.
func (p *prober) recordStreamObservation(parsed map[string]interface{}, statusCode int, latencyMs int64) {
	if p.tel == nil || parsed == nil {
		return
	}
	// Normalize through JSON so numbers are float64 (as a real parsed response is)
	// — a synthesized envelope built with Go ints would otherwise fail isNonNegInt
	// / intField, producing spurious nonneg-int issues in the passive validators.
	if b, err := common.Marshal(parsed); err == nil {
		var normalized map[string]interface{}
		if common.Unmarshal(b, &normalized) == nil {
			parsed = normalized
		}
	}
	p.tel.mu.Lock()
	defer p.tel.mu.Unlock()
	p.tel.observations = append(p.tel.observations, observation{
		response:   parsed,
		statusCode: statusCode,
		latencyMs:  latencyMs,
	})
}

// countTokens calls POST /v1/messages/count_tokens (Anthropic input-token
// cross-check for token_usage). It is NOT observed (the response shape is not a
// Messages object and would trip the passive protocol detector). The endpoint
// rejects Messages-only fields, so stream/max_tokens/temperature are stripped
// first (Veridrop client.count_tokens pops the same three).
func (p *prober) countTokens(ctx context.Context, headers map[string]string, payload any) httpResult {
	if body, ok := payload.(map[string]interface{}); ok {
		clone := make(map[string]interface{}, len(body))
		for k, v := range body {
			switch k {
			case "stream", "max_tokens", "temperature":
				continue
			}
			clone[k] = v
		}
		payload = clone
	}
	return p.doWithTimeout(ctx, "/v1/messages/count_tokens", headers, payload, false, p.timeout, maxAttempts(p.protocol))
}

// doWithTimeout performs a probe under a per-request timeout, retrying up to
// `limit` total attempts, and records telemetry + (optionally) an observation
// for the passive bus. limit=1 disables retry (used for stream probes).
func (p *prober) doWithTimeout(ctx context.Context, path string, headers map[string]string, payload any, observe bool, reqTimeout time.Duration, limit int) httpResult {
	payload = sanitizeProbeBody(payload)
	bodyBytes, err := common.Marshal(payload)
	if err != nil {
		return httpResult{err: fmt.Errorf("marshal payload: %w", err)}
	}
	fullURL := p.buildURL(path)

	if limit < 1 {
		limit = 1
	}
	var res httpResult
	backoffs := 0
	attempts := 0
	for attempt := 1; attempt <= limit; attempt++ {
		attempts++
		res = p.doOnce(ctx, fullURL, headers, bodyBytes, reqTimeout)
		if res.err == nil && !retryableStatus(p.protocol, res.statusCode) {
			break // success or non-retryable status
		}
		if ctx.Err() != nil {
			break // caller cancelled/deadline — retrying won't help
		}
		if attempt == limit {
			break
		}
		backoffs++
		select {
		case <-ctx.Done():
		case <-time.After(backoffDelay(attempt, res.header)):
		}
		if ctx.Err() != nil {
			break
		}
	}
	if p.tel != nil {
		p.tel.record(res, observe, backoffs, attempts)
	}
	return res
}

// doOnce performs a single HTTP probe (no retry) under a per-request timeout.
func (p *prober) doOnce(ctx context.Context, fullURL string, headers map[string]string, bodyBytes []byte, reqTimeout time.Duration) httpResult {
	start := time.Now()
	res := httpResult{}

	reqCtx := ctx
	if reqTimeout > 0 {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, reqTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		res.err = fmt.Errorf("build request: %w", err)
		res.durationMs = time.Since(start).Milliseconds()
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		res.err = err
		res.durationMs = time.Since(start).Milliseconds()
		return res
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	res.statusCode = resp.StatusCode
	res.header = resp.Header
	res.body = data
	res.text = string(data)
	res.durationMs = time.Since(start).Milliseconds()

	var m map[string]interface{}
	if common.Unmarshal(data, &m) == nil {
		res.parsed = m
	}
	return res
}

// backoffDelay computes the wait before a retry: Retry-After when the upstream
// sends a sane value, otherwise capped exponential backoff.
func backoffDelay(attempt int, header http.Header) time.Duration {
	if header != nil {
		if ra := strings.TrimSpace(header.Get("Retry-After")); ra != "" {
			// Veridrop honors a fractional Retry-After (float seconds), capped.
			if secs, err := strconv.ParseFloat(ra, 64); err == nil && secs >= 0 {
				d := time.Duration(secs * float64(time.Second))
				if d > probeRetryAfterCap {
					d = probeRetryAfterCap
				}
				return d
			}
		}
	}
	// Exponential min(2^(attempt-1), 30s) — attempt is 1-based here, matching
	// Veridrop's 0-based min(2^attempt, MAX_BACKOFF_S).
	d := time.Duration(1<<(attempt-1)) * time.Second
	if d > probeBackoffMax {
		d = probeBackoffMax
	}
	return d
}

// upstreamErrorText extracts a human-readable error message from a non-2xx
// probe response so scorer.fatalRunError can classify quota/model failures.
func upstreamErrorText(res httpResult) string {
	if res.err != nil {
		return res.err.Error()
	}
	if res.parsed != nil {
		if errObj, ok := res.parsed["error"].(map[string]interface{}); ok {
			parts := make([]string, 0, 2)
			if code := asString(errObj["type"]); code != "" {
				parts = append(parts, code)
			}
			if code := asString(errObj["code"]); code != "" {
				parts = append(parts, code)
			}
			if msg := asString(errObj["message"]); msg != "" {
				parts = append(parts, msg)
			}
			if len(parts) > 0 {
				return strings.Join(parts, ": ")
			}
		}
		if msg := asString(res.parsed["message"]); msg != "" {
			return msg
		}
	}
	if len(res.text) > 0 {
		if len(res.text) > 500 {
			return res.text[:500]
		}
		return res.text
	}
	return fmt.Sprintf("HTTP %d", res.statusCode)
}

// --- generic JSON access helpers (bodies decode to map[string]interface{}) ---

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func mapField(m map[string]interface{}, key string) interface{} {
	if m == nil {
		return nil
	}
	return m[key]
}

func strField(m map[string]interface{}, key string) string {
	return asString(mapField(m, key))
}

func subMap(m map[string]interface{}, key string) map[string]interface{} {
	sm, _ := mapField(m, key).(map[string]interface{})
	return sm
}

func subSlice(m map[string]interface{}, key string) []interface{} {
	s, _ := mapField(m, key).([]interface{})
	return s
}

func numField(m map[string]interface{}, key string) (float64, bool) {
	f, ok := mapField(m, key).(float64)
	return f, ok
}

// sseDataObjects parses an SSE body into the sequence of JSON objects carried
// on its "data:" lines (skipping "[DONE]" and non-JSON payloads).
func sseDataObjects(raw string) []map[string]interface{} {
	var out []map[string]interface{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var m map[string]interface{}
		if common.UnmarshalJsonStr(payload, &m) == nil {
			out = append(out, m)
		}
	}
	return out
}
