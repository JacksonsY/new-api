package detector

import (
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

// probe retry policy (mirrors Veridrop ThrottledClient: retry transient
// 429/5xx/529 + timeout/network with capped exponential backoff + Retry-After).
const (
	probeMaxAttempts   = 3
	probeBackoffBase   = 500 * time.Millisecond
	probeBackoffMax    = 8 * time.Second
	probeRetryAfterCap = 15 * time.Second
)

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

func (t *runTelemetry) record(res httpResult, observe bool, backoffs int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requestCount++
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
		if u, ok := o.response["usage"].(map[string]interface{}); ok {
			if v, ok := intField(u, "input_tokens"); ok {
				inTok += v
			}
			if v, ok := intField(u, "output_tokens"); ok {
				outTok += v
			}
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

// retryableStatus reports whether an HTTP status warrants a probe retry.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout,      // 504
		529:                            // Anthropic overloaded_error
		return true
	}
	return false
}

// prober performs the outbound HTTP probes against the target relay. It is
// constructed with an SSRF-guarded HTTP client that never follows redirects.
type prober struct {
	client  *http.Client
	baseURL string
	apiKey  string
	timeout time.Duration
	tel     *runTelemetry
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
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		// Control runs after DNS resolution with the concrete IP:port about to
		// be dialed, so it blocks DNS-rebinding into private ranges.
		Control: func(_, address string, _ syscall.RawConn) error {
			return guardDialAddress(address)
		},
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		// Never follow redirects: a 30x could point into a private range, and
		// following it would bypass the up-front host check.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &prober{
		client:  client,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		timeout: timeout,
		tel:     &runTelemetry{},
	}, nil
}

// guardBaseURL validates a user-supplied base URL before any request is made:
// it must be http(s), have a host, and must not resolve to a loopback,
// private, link-local (incl. 169.254.169.254 cloud metadata), unique-local
// IPv6, or unspecified address. Resolution failures are treated as fatal
// (fail closed).
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
	return false
}

// buildURL joins the base URL with a path suffix, avoiding a duplicated "/v1"
// segment when the operator's base URL already ends in "/v1".
func buildURL(base, suffix string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(suffix, "/v1/") {
		return base + strings.TrimPrefix(suffix, "/v1")
	}
	return base + suffix
}

// postJSON sends a JSON POST (with retry/backoff on transient 429/5xx/529 and
// transport errors), reads the capped body, parses JSON objects, and records the
// response on the passive-observation bus. Transport errors are returned in
// httpResult.err; the caller decides how to score them.
func (p *prober) postJSON(ctx context.Context, path string, headers map[string]string, payload any) httpResult {
	return p.do(ctx, path, headers, payload, true)
}

// postSSE sends a streaming (stream:true) JSON POST and returns the raw
// event-stream body as text. Streams are NOT observed by passive detectors
// (Veridrop broadcasts only non-stream responses).
func (p *prober) postSSE(ctx context.Context, path string, headers map[string]string, payload any) httpResult {
	sseHeaders := map[string]string{"Accept": "text/event-stream"}
	for k, v := range headers {
		sseHeaders[k] = v
	}
	return p.do(ctx, path, sseHeaders, payload, false)
}

// countTokens calls POST /v1/messages/count_tokens (Anthropic input-token
// cross-check for token_usage). It is NOT observed (the response shape is not a
// Messages object and would trip the passive protocol detector).
func (p *prober) countTokens(ctx context.Context, headers map[string]string, payload any) httpResult {
	return p.do(ctx, "/v1/messages/count_tokens", headers, payload, false)
}

// do performs a probe with retry/backoff and records telemetry + (optionally) an
// observation for the passive bus.
func (p *prober) do(ctx context.Context, path string, headers map[string]string, payload any, observe bool) httpResult {
	bodyBytes, err := common.Marshal(payload)
	if err != nil {
		return httpResult{err: fmt.Errorf("marshal payload: %w", err)}
	}
	fullURL := buildURL(p.baseURL, path)

	var res httpResult
	backoffs := 0
	for attempt := 1; attempt <= probeMaxAttempts; attempt++ {
		res = p.doOnce(ctx, fullURL, headers, bodyBytes)
		if res.err == nil && !retryableStatus(res.statusCode) {
			break // success or non-retryable status
		}
		if ctx.Err() != nil {
			break // caller cancelled/deadline — retrying won't help
		}
		if attempt == probeMaxAttempts {
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
		p.tel.record(res, observe, backoffs)
	}
	return res
}

// doOnce performs a single HTTP probe (no retry).
func (p *prober) doOnce(ctx context.Context, fullURL string, headers map[string]string, bodyBytes []byte) httpResult {
	start := time.Now()
	res := httpResult{}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(bodyBytes))
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
			if secs, err := strconv.Atoi(ra); err == nil && secs >= 0 {
				d := time.Duration(secs) * time.Second
				if d > probeRetryAfterCap {
					d = probeRetryAfterCap
				}
				return d
			}
		}
	}
	d := probeBackoffBase << (attempt - 1)
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
