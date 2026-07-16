package detector

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

// Detector names (stable identifiers used in results and by the caller).
const (
	detectorIdentity          = "identity"
	detectorConsistency       = "consistency"
	detectorMessageID         = "message_id"
	detectorProtocol          = "protocol"
	detectorThinkingSignature = "thinking_signature"
	detectorIntegrity         = "integrity"
	detectorTokenUsage        = "token_usage"
	detectorBasicRequest      = "basic_request"
	detectorModelConsistency  = "model_consistency"
	detectorModelInfo         = "model_info"
	detectorFunctionCalling   = "function_calling"
	detectorLongContext       = "long_context"
	// Tier B / structured-output / token detectors
	detectorStructuredOutput  = "structured_output"
	detectorKnowledge         = "knowledge"
	detectorBehavioral        = "behavioral_signature"
	detectorPDF               = "pdf"
	detectorTokenBilling      = "token_billing"
	detectorTokenParity       = "token_parity"
	detectorBackendOrigin     = "backend_origin"
	detectorSystemPromptLeak  = "system_prompt_leak"
	detectorStopSequence      = "stop_sequence"
	detectorMaxTokensBehav    = "max_tokens"
	detectorErrorShape        = "error_shape"
	detectorCacheBehavior     = "cache_behavior"
	detectorMultiTurn         = "multi_turn"
	detectorHeaderFinger      = "header_fingerprint"
	detectorInjectionResist   = "injection_resistance"
	detectorResponseIntegrity = "response_integrity"
	detectorWeb3Safety        = "web3_safety"
	detectorErrorLeakage      = "error_leakage_scan"
	detectorSupplyChain       = "supply_chain_integrity"
	detectorExfilScan         = "exfil_channel_scan"
	detectorAdaptiveInjection = "adaptive_injection"
	detectorHiddenPromptFloor = "hidden_prompt_floor"
	detectorUnicodeFidelity   = "unicode_fidelity"
	detectorSensitiveLeak     = "sensitive_data_leak"
	detectorPkgSubstitution   = "pkg_substitution"
	detectorStreamIntegrity   = "stream_integrity"
	detectorSpeed             = "speed"
	detectorResponsesAPI      = "responses_api"
	detectorResponsesFunc     = "responses_function_calling"
)

// mode ordering: a detector with minMode <= the run's mode is selected.
const (
	modeRankQuick    = 0
	modeRankStandard = 1
	modeRankFull     = 2
)

func modeRank(mode string) int {
	switch mode {
	case ModeQuick:
		return modeRankQuick
	case ModeFull:
		return modeRankFull
	default:
		return modeRankStandard
	}
}

// overallTimeout is the whole-run budget for a mode. quick/standard keep
// Veridrop's ExecutionConfig.for_mode values (60s/120s). full deviates from
// Veridrop's 180s: this port roughly tripled the full-mode battery (identity,
// backend origin, and the LLMprobe/api-relay-audit security suites), and at the
// default concurrency of 3 a genuine reasoning-model relay (gpt-5.x / grok-4.x /
// adaptive Claude, 10–30s per call) can legitimately need well beyond 180s.
// Deadline casualties no longer drag the score down (runOne reclassifies them to
// skip), so a larger budget here only improves completeness — more of the
// battery actually finishes — without risking a false verdict. Long-context runs
// stay exempt and are bounded per-request instead.
func overallTimeout(mode string) time.Duration {
	switch mode {
	case ModeQuick:
		return 60 * time.Second
	case ModeFull:
		return 300 * time.Second
	default:
		return 120 * time.Second
	}
}

// detectorFn runs one detector. It sets Status/Score/Details/Error; the runner
// fills in Name/DisplayName/Weight/DurationMs.
type detectorFn func(ctx context.Context, p *prober, cfg Config) DetectorResult

// detectorDef describes a detector and when it runs.
type detectorDef struct {
	name        string
	displayName string
	weight      float64
	minMode     int  // lowest mode rank at which this detector runs
	longContext bool // gated behind Config.IncludeLongContext
	fn          detectorFn
}

// selectDetectors returns the detectors to run for a normalized config, applying
// protocol dispatch and mode/long-context gating.
func selectDetectors(cfg Config) []detectorDef {
	var all []detectorDef
	switch cfg.Protocol {
	case ProtocolAnthropic:
		all = anthropicDetectors()
	case ProtocolGemini:
		all = geminiDetectors()
	default:
		// openai — and grok, which speaks the same chat surface and shares the
		// full openai battery (model-aware calibration keys off cfg.Model).
		all = openaiDetectors()
	}

	rank := modeRank(cfg.Mode)
	selected := make([]detectorDef, 0, len(all))
	for _, d := range all {
		if d.longContext {
			// Long-context detectors only run in full mode or when explicitly
			// opted in (standard or extreme tier); otherwise they self-skip.
			if cfg.Mode != ModeFull && !cfg.IncludeLongContext && !cfg.IncludeLongContextExtreme {
				continue
			}
			selected = append(selected, d)
			continue
		}
		if d.minMode <= rank {
			selected = append(selected, d)
		}
	}
	return selected
}

// isPassiveDetector reports whether a detector validates the run's collected
// observations instead of issuing its own probe. Passive detectors (protocol,
// message_id) run in a second phase, after active detectors have populated the
// observation bus — mirroring Veridrop's PassiveDetector broadcast model.
func isPassiveDetector(name string) bool {
	return name == detectorProtocol || name == detectorMessageID
}

// runDetectors executes the selected detectors in two phases and returns their
// results in definition order. Phase 1 runs active detectors concurrently
// (their probes populate the observation bus); phase 2 runs passive detectors
// that finalize over those observations. A panic in one detector becomes an
// "error" result and never aborts the run.
func runDetectors(ctx context.Context, p *prober, cfg Config, defs []detectorDef) []DetectorResult {
	// Bound the whole run by the mode's overall timeout. Long-context runs are
	// exempt: their tiers legitimately take minutes and are bounded per-request
	// instead. When the budget fires, in-flight and not-yet-started detectors are
	// cancelled; runOne reclassifies those deadline casualties to skip (excluded
	// from scoring) rather than error, so a genuine relay that simply ran out of
	// time is not scored down for detectors that never got to run.
	if !cfg.IncludeLongContext && !cfg.IncludeLongContextExtreme {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, overallTimeout(cfg.Mode))
		defer cancel()
	}

	results := make([]DetectorResult, len(defs))

	limit := cfg.MaxConcurrent
	if limit <= 0 {
		limit = 3
	}

	runPhase := func(indices []int) {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(limit)
		for _, idx := range indices {
			idx := idx
			d := defs[idx]
			g.Go(func() error {
				results[idx] = runOne(gctx, p, cfg, d)
				return nil // never propagate: one detector must not cancel the rest
			})
		}
		_ = g.Wait()
	}

	var active, passive []int
	for i := range defs {
		if isPassiveDetector(defs[i].name) {
			passive = append(passive, i)
		} else {
			active = append(active, i)
		}
	}
	runPhase(active)
	runPhase(passive)
	return results
}

// runOne executes a single detector, recovering from panics and stamping the
// definition's identity/weight and the measured duration onto the result.
func runOne(ctx context.Context, p *prober, cfg Config, d detectorDef) (res DetectorResult) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			res = DetectorResult{Status: "error", Score: 0, Error: fmt.Sprintf("panic: %v", r)}
		}
		res.Name = d.name
		res.DisplayName = d.displayName
		res.Weight = d.weight
		if res.DurationMs == 0 {
			res.DurationMs = time.Since(start).Milliseconds()
		}
	}()
	res = d.fn(ctx, p, cfg)
	// A detector that errored only because the overall run budget (or an external
	// cancel) fired is a casualty of the deadline, not evidence about the model.
	// gctx is derived from the overall-timeout context and is only cancelled by
	// it (detectors never propagate errors), so ctx.Err() != nil here means the
	// budget/cancel fired. Reclassify to skip: computeTotal excludes skip, whereas
	// an "error" would be averaged in at score 0 with full weight and drag a
	// genuine relay's verdict to marginal/failed whenever the full-mode battery
	// simply ran out of time. A detector that already errored while the budget was
	// still alive keeps its error status and its 0 (that is real evidence).
	if res.Status == "error" && ctx.Err() != nil {
		res = detectorSkip("超出本次检测时间预算，该检测器未跑完（不计入评分）")
	}
	return res
}
