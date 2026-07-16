// Package detector is a native Go port of the Veridrop AI-API-relay
// authenticity detector (github.com/canarybyte/veridrop, AGPL-3.0). It runs a
// battery of small, real requests against a target relay (base_url + api_key +
// model) and scores how closely the responses match the genuine upstream
// provider's protocol, identity, and fingerprints.
//
// Only the rule-based "Tier A" detectors are implemented here: they need no
// per-model baseline and carry the strongest signal for the common
// "swapped-core / stripped-capability / non-conforming protocol" relay fakes.
//
// Attribution: detection logic and the brand/fingerprint tables are ported from
// Veridrop (AGPL-3.0). This attribution is additive and does not replace any
// new-api / QuantumNous project identity.
package detector

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Config is the frozen input contract. See package doc for semantics.
type Config struct {
	BaseURL                   string
	APIKey                    string
	Model                     string
	Protocol                  string // "anthropic" | "openai" | "gemini" | "" (auto: infer from model name)
	Mode                      string // "quick" | "standard" | "full" (default "standard")
	IncludeLongContext        bool   // opt-in; when false, long-context detectors self-skip
	IncludeLongContextExtreme bool   // opt-in "极限档": adaptive tiers up to the model's advertised limit
	TimeoutSeconds            int    // per-request timeout; default 30
	MaxConcurrent             int    // default 3

	// Transport, when non-nil, is the HTTP transport probes egress through — the
	// caller sets it to the fork's global-proxy client transport so detection
	// honors the configured outbound proxy (+ direct fallback). When nil, the
	// prober builds a direct connection with a dial-time SSRF re-resolution guard.
	// Either way guardBaseURL is the up-front SSRF floor. Not part of the frozen
	// wire contract; it is a per-run transport injection.
	Transport http.RoundTripper
}

// Report is the frozen output contract. The caller marshals it with
// common.Marshal and stores it; keep these JSON tags stable.
type Report struct {
	Protocol     string `json:"protocol"`
	BaseURL      string `json:"base_url"`
	APIKeyMasked string `json:"api_key_masked"`
	TargetModel  string `json:"target_model"`
	Mode         string `json:"mode"`
	// ProbedEndpoint is the primary upstream path the shared probes hit for this
	// run — /v1/messages (Claude), the native generateContent path (Gemini), or
	// /v1/responses vs /v1/chat/completions (OpenAI/Grok, resolved by preference).
	ProbedEndpoint       string              `json:"probed_endpoint,omitempty"`
	Timestamp            int64               `json:"timestamp"` // unix seconds; caller sets if 0
	TotalScore           float64             `json:"total_score"`
	Verdict              string              `json:"verdict"` // "passed" | "marginal" | "failed"
	CriticalCount        int                 `json:"critical_count"`
	Results              []DetectorResult    `json:"results"`
	Summary              string              `json:"summary"`
	SelfReportedIdentity string              `json:"self_reported_identity,omitempty"`
	DetectedBrands       []string            `json:"detected_brands,omitempty"`
	BackendOrigin        string              `json:"backend_origin,omitempty"` // Claude 真实后端(cc-proxy-detector 溯源)
	Performance          *PerformanceMetrics `json:"performance,omitempty"`
	BaselineComparison   *BaselineComparison `json:"baseline_comparison,omitempty"`
	RunError             string              `json:"run_error,omitempty"`
}

// PerformanceMetrics is the per-run telemetry block (Veridrop PerformanceMetrics):
// request count, aggregate/first-request latency, aggregate token usage, and the
// number of retry backoffs the probes had to absorb.
type PerformanceMetrics struct {
	RequestCount    int     `json:"request_count"`
	TotalLatencyMs  int64   `json:"total_latency_ms"`
	TtftMs          int64   `json:"ttft_ms"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	TokensPerSecond float64 `json:"tokens_per_second"`
	BackoffEvents   int     `json:"backoff_events"`
}

// DetectorResult is the frozen per-detector output contract.
type DetectorResult struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"display_name"`
	Status      string                 `json:"status"` // "pass" | "fail" | "skip" | "error"
	Score       float64                `json:"score"`  // 0..100
	Weight      float64                `json:"weight"`
	Details     map[string]interface{} `json:"details,omitempty"`
	DurationMs  int64                  `json:"duration_ms,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

// Detection modes.
const (
	ModeQuick    = "quick"
	ModeStandard = "standard"
	ModeFull     = "full"
)

// Supported protocols. Grok (xAI) speaks the OpenAI-compatible chat surface, so
// it shares the openai detector battery and prober behavior end to end; keeping
// it a first-class protocol string preserves the user's intent in the report
// and lets model-aware calibration (reasoning billing, UUID response ids,
// identity vendor expectation) anchor on what the endpoint CLAIMS to serve.
const (
	ProtocolAnthropic = "anthropic"
	ProtocolOpenAI    = "openai"
	ProtocolGemini    = "gemini"
	ProtocolGrok      = "grok"
)

// Run executes the detection battery and returns a report. It never panics;
// transport/guard failures surface as report.RunError with a low/zero score.
//
// The returned error is always nil: even guard/transport failures are recorded
// in the report (RunError + "failed" verdict + zero score) so the caller can
// persist the outcome of a failed detection. Callers should inspect the report
// rather than branch on the error.
func Run(ctx context.Context, cfg Config) (*Report, error) {
	cfg = normalizeConfig(cfg)

	report := &Report{
		Protocol:     cfg.Protocol,
		BaseURL:      cfg.BaseURL,
		APIKeyMasked: maskAPIKey(cfg.APIKey),
		TargetModel:  cfg.Model,
		Mode:         cfg.Mode,
		Timestamp:    time.Now().Unix(),
	}

	prb, err := newProber(cfg)
	if err != nil {
		// SSRF guard / client construction failed: nothing was sent.
		report.RunError = err.Error()
		report.TotalScore = 0
		report.Verdict = "failed"
		report.Summary = summaryText(0, "failed")
		return report, nil
	}

	defs := selectDetectors(cfg)
	// Resolve the preferred OpenAI/Grok surface (Responses vs Chat Completions)
	// once, before the concurrent detector phase, so the shared probes dispatch
	// consistently.
	prb.resolveOpenAISurface(ctx, cfg.Model)
	report.ProbedEndpoint = primaryEndpoint(cfg, prb.openaiSurface)
	results := runDetectors(ctx, prb, cfg, defs)

	report.Results = results

	// A quota/billing/model-unavailable failure upstream means the account
	// simply cannot run the test. Force score 0 / verdict failed and surface the
	// actionable message as the summary, rather than persist a misleading partial
	// score+verdict (Veridrop cli: total = 0 if run_error; verdict =
	// effective_verdict(total); summary = run_error or summary_text).
	fatal := fatalRunError(results)
	if fatal != "" {
		report.RunError = fatal
		report.TotalScore = 0
	} else {
		report.TotalScore = computeTotal(results)
	}
	report.Verdict = effectiveVerdict(report.TotalScore, results)
	report.CriticalCount = criticalCount(results)
	if fatal != "" {
		report.Summary = fatal
	} else {
		report.Summary = summaryText(report.TotalScore, report.Verdict)
	}

	applyIdentityFindings(report, results)
	applyBackendOrigin(report, results)
	report.Performance = prb.performance()

	// Baseline comparison: a full-mode run of a known Claude model is diffed
	// against the embedded ground-truth baseline. Relative criticals (signature
	// dropped, new backend brand, model mismatch, tool_use stripped, …) are the
	// smoking guns a single absolute run cannot emit; a critical downgrades a
	// passed verdict and drives the caller's critical-based handling. Skipped for
	// an invalid (fatal) run — there is nothing meaningful to diff.
	if fatal == "" {
		if baseline := findBaseline(cfg.Model, cfg.Mode); baseline != nil {
			cmp := compareToBaseline(baseline, report)
			report.BaselineComparison = cmp
			if cmp.CriticalCount > 0 {
				if cmp.CriticalCount > report.CriticalCount {
					report.CriticalCount = cmp.CriticalCount
				}
				if report.Verdict == "passed" {
					report.Verdict = "marginal"
					report.Summary = summaryText(report.TotalScore, "marginal")
				}
			}
		}
	}
	return report, nil
}

// normalizeConfig fills defaults and infers the protocol from the model name
// when unset, mirroring Veridrop's auto-detection.
func normalizeConfig(cfg Config) Config {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)

	switch strings.ToLower(strings.TrimSpace(cfg.Protocol)) {
	case ProtocolAnthropic, ProtocolOpenAI, ProtocolGemini, ProtocolGrok:
		cfg.Protocol = strings.ToLower(strings.TrimSpace(cfg.Protocol))
	default:
		cfg.Protocol = inferProtocol(cfg.Model)
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case ModeQuick, ModeStandard, ModeFull:
		cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	default:
		cfg.Mode = ModeStandard
	}

	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}
	return cfg
}

// primaryEndpoint reports the main upstream path the shared probes hit, for the
// report so a user sees which native surface was tested. openaiSurface is the
// resolved OpenAI/Grok preference ("" before resolution → chat default).
func primaryEndpoint(cfg Config, openaiSurface string) string {
	switch cfg.Protocol {
	case ProtocolAnthropic:
		return anthropicMessagesPath
	case ProtocolGemini:
		return geminiNativePath(cfg.Model, false)
	default: // openai / grok
		if openaiSurface == surfaceResponses {
			return responsesPath
		}
		return openaiChatPath
	}
}

// inferProtocol maps a model name to a protocol: names containing "claude" →
// anthropic, "gemini" → gemini, "grok" → grok, everything else → openai.
func inferProtocol(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude"):
		return ProtocolAnthropic
	case strings.Contains(m, "gemini"):
		return ProtocolGemini
	case strings.Contains(m, "grok"):
		return ProtocolGrok
	default:
		return ProtocolOpenAI
	}
}

// maskAPIKey keeps the first and last few characters so a stored report can be
// correlated to a key without leaking it (Veridrop's api_key_masked).
func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	// Match Veridrop mask_api_key exactly: 'sk-y7xUabc...0h' -> 'sk-y7xU••••••0h'.
	// Operate on runes so a multibyte key is not split mid-character.
	r := []rune(key)
	if len(r) <= 8 {
		head := 2
		if head > len(r) {
			head = len(r)
		}
		return string(r[:head]) + strings.Repeat("•", len(r)-head)
	}
	return string(r[:6]) + strings.Repeat("•", 6) + string(r[len(r)-2:])
}

// applyBackendOrigin lifts the resolved Claude backend (Anthropic direct /
// Bedrock / Vertex / suspected spoof) out of the backend_origin detector's
// result onto the report, so the caller/UI can surface it prominently.
func applyBackendOrigin(report *Report, results []DetectorResult) {
	for _, r := range results {
		if r.Name != detectorBackendOrigin || r.Details == nil {
			continue
		}
		if s, ok := r.Details["backend"].(string); ok {
			report.BackendOrigin = s
		}
		return
	}
}

// applyIdentityFindings lifts the self-reported identity string and any detected
// non-official brands out of the identity detector's result onto the report.
func applyIdentityFindings(report *Report, results []DetectorResult) {
	for _, r := range results {
		if r.Name != detectorIdentity || r.Details == nil {
			continue
		}
		if s, ok := r.Details["self_reported_identity"].(string); ok {
			report.SelfReportedIdentity = s
		}
		if brands, ok := r.Details["detected_non_anthropic_brands"].([]string); ok && len(brands) > 0 {
			report.DetectedBrands = brands
		}
		// chat 面(openai/grok/gemini)身份检测器的异源品牌键。
		if brands, ok := r.Details["detected_foreign_brands"].([]string); ok && len(brands) > 0 {
			report.DetectedBrands = brands
		}
		return
	}
}
