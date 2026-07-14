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
}

// Report is the frozen output contract. The caller marshals it with
// common.Marshal and stores it; keep these JSON tags stable.
type Report struct {
	Protocol             string              `json:"protocol"`
	BaseURL              string              `json:"base_url"`
	APIKeyMasked         string              `json:"api_key_masked"`
	TargetModel          string              `json:"target_model"`
	Mode                 string              `json:"mode"`
	Timestamp            int64               `json:"timestamp"` // unix seconds; caller sets if 0
	TotalScore           float64             `json:"total_score"`
	Verdict              string              `json:"verdict"` // "passed" | "marginal" | "failed"
	CriticalCount        int                 `json:"critical_count"`
	Results              []DetectorResult    `json:"results"`
	Summary              string              `json:"summary"`
	SelfReportedIdentity string              `json:"self_reported_identity,omitempty"`
	DetectedBrands       []string            `json:"detected_brands,omitempty"`
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

// Supported protocols.
const (
	ProtocolAnthropic = "anthropic"
	ProtocolOpenAI    = "openai"
	ProtocolGemini    = "gemini"
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
	results := runDetectors(ctx, prb, cfg, defs)

	report.Results = results
	report.TotalScore = computeTotal(results)
	verdict := effectiveVerdict(report.TotalScore, results)
	report.Verdict = verdict
	report.CriticalCount = criticalCount(results)
	report.Summary = summaryText(report.TotalScore, verdict)

	// A quota/billing/model-unavailable failure upstream means the account
	// simply cannot run the test; surface that instead of a misleading score.
	if fatal := fatalRunError(results); fatal != "" {
		report.RunError = fatal
	}

	applyIdentityFindings(report, results)
	report.Performance = prb.performance()

	// Baseline comparison: a full-mode run of a known Claude model is diffed
	// against the embedded ground-truth baseline. Relative criticals (signature
	// dropped, new backend brand, model mismatch, tool_use stripped, …) are the
	// smoking guns a single absolute run cannot emit; a critical downgrades a
	// passed verdict and drives the caller's critical-based handling.
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
	return report, nil
}

// normalizeConfig fills defaults and infers the protocol from the model name
// when unset, mirroring Veridrop's auto-detection.
func normalizeConfig(cfg Config) Config {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)

	switch strings.ToLower(strings.TrimSpace(cfg.Protocol)) {
	case ProtocolAnthropic, ProtocolOpenAI, ProtocolGemini:
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

// inferProtocol maps a model name to a protocol: names containing "claude" →
// anthropic, "gemini" → gemini, everything else → openai.
func inferProtocol(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude"):
		return ProtocolAnthropic
	case strings.Contains(m, "gemini"):
		return ProtocolGemini
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
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + strings.Repeat("*", 6) + key[len(key)-4:]
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
		return
	}
}
