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
	detectorStructuredOutput = "structured_output"
	detectorKnowledge        = "knowledge"
	detectorBehavioral       = "behavioral_signature"
	detectorPDF              = "pdf"
	detectorTokenBilling     = "token_billing"
	detectorTokenParity      = "token_parity"
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
	return res
}
