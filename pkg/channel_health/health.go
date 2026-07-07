// Package channelhealth implements passive, self-adaptive channel health for
// routing. It learns each channel's first-token latency (TTFT) and
// channel-fault error rate purely from real relay traffic — never by probing
// upstream — and exposes:
//
//   - Select:               health-weighted candidate selection within a
//     priority layer (TopK + weighted random).
//   - ShouldEscapeAffinity: whether an affinity-anchored channel has degraded
//     enough to abandon its warm prompt cache.
//   - A passive 3-state circuit breaker (closed/half-open/open) that opens on
//     consecutive channel-fault failures and recovers from real traffic.
//
// All state is in-memory and per-instance; signals are soft routing hints, so
// per-instance EWMA convergence is acceptable (each replica sees similar
// traffic). See setting/operation_setting/adaptive_routing_setting.go.
package channelhealth

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type circuitState int32

const (
	circuitClosed circuitState = iota
	circuitHalfOpen
	circuitOpen
)

// rollingWindow is a fixed 60-second ring of per-second counters used for RPM /
// TPM. Each bucket records the unix-second it currently represents; a stale
// bucket is zeroed lazily on write, so there is no sweeper goroutine and sum()
// simply skips any bucket outside the trailing 60s. The owning channelStat's mu
// serializes all access.
const rollingWindowSeconds = 60

type rollingWindow struct {
	counts [rollingWindowSeconds]int64
	stamps [rollingWindowSeconds]int64 // unix second each bucket currently holds
}

func (w *rollingWindow) add(nowSec, n int64) {
	idx := nowSec % rollingWindowSeconds
	if w.stamps[idx] != nowSec {
		w.stamps[idx] = nowSec
		w.counts[idx] = 0
	}
	w.counts[idx] += n
}

func (w *rollingWindow) sum(nowSec int64) int64 {
	cutoff := nowSec - rollingWindowSeconds + 1
	var total int64
	for i := 0; i < rollingWindowSeconds; i++ {
		if w.stamps[i] >= cutoff && w.stamps[i] <= nowSec {
			total += w.counts[i]
		}
	}
	return total
}

type channelStat struct {
	// inflight is the number of requests this instance currently has in flight
	// to the channel. Atomic, independent of mu, updated on the relay hot path.
	inflight atomic.Int64

	// channelID is immutable after creation (set in getStat); read without mu.
	channelID int

	mu sync.Mutex

	hasErr   bool
	errEWMA  float64 // [0,1]
	hasTtft  bool
	ttftEWMA float64 // ms

	// Observability-only signals (never affect routing/circuit). Fed by
	// ReportTraffic, surfaced through StatView for the ops-monitor page.
	hasLatency  bool
	latencyEWMA float64 // ms, total response time
	hasTps      bool
	tpsEWMA     float64 // output tokens/sec
	lastUsedAt  int64   // unix seconds; 0 = never observed
	lastErrCode int     // last channel-fault HTTP status; 0 = none
	lastErrAt   int64   // unix seconds; 0 = none

	// Rolling 60s windows (RPM / TPM) and cumulative channel-fault tallies by
	// class (429 rate-limit / 5xx server / other incl. network & code 0).
	reqWindow rollingWindow
	tokWindow rollingWindow
	err429    int64
	err5xx    int64
	errOther  int64

	consecutiveFailures int
	state               circuitState
	openUntil           time.Time
}

var stats sync.Map // channelID(int) -> *channelStat

func getStat(id int) *channelStat {
	if v, ok := stats.Load(id); ok {
		return v.(*channelStat)
	}
	actual, _ := stats.LoadOrStore(id, &channelStat{channelID: id, state: circuitClosed})
	return actual.(*channelStat)
}

// Enabled reports whether adaptive routing is turned on.
func Enabled() bool {
	return operation_setting.GetAdaptiveRoutingSetting().Enabled
}

// AcquireInflight marks one request as in flight to a channel (call before the
// upstream attempt). Pair with ReleaseInflight. No-op when adaptive routing is
// off, so there is zero hot-path cost until the feature is enabled.
func AcquireInflight(channelID int) {
	if channelID <= 0 || !operation_setting.GetAdaptiveRoutingSetting().Enabled {
		return
	}
	getStat(channelID).inflight.Add(1)
}

// ReleaseInflight marks one in-flight request as finished. Safe to call even
// when adaptive routing is off; it clamps at zero so a stray release cannot
// drive the counter negative.
func ReleaseInflight(channelID int) {
	if channelID <= 0 {
		return
	}
	v, ok := stats.Load(channelID)
	if !ok {
		return
	}
	s := v.(*channelStat)
	// Decrement, clamped at zero via CAS so a stray release (only possible if the
	// feature is toggled on mid-request) can neither go negative nor clobber a
	// concurrent AcquireInflight increment.
	for {
		cur := s.inflight.Load()
		if cur <= 0 {
			return
		}
		if s.inflight.CompareAndSwap(cur, cur-1) {
			return
		}
	}
}

// ReportResult feeds one relay attempt's outcome into a channel's health.
//
//	success      — the attempt returned a usable response.
//	channelFault — for failures, whether the fault is attributable to the
//	               channel/upstream (5xx/429/network) rather than the client
//	               (400/402/quota). Client-fault failures are ignored entirely,
//	               so a channel is never blamed for user errors.
//	ttftMs       — first-token latency in ms; <=0 means not measured.
func ReportResult(channelID int, success bool, channelFault bool, ttftMs int64) {
	if channelID <= 0 {
		return
	}
	setting := operation_setting.GetAdaptiveRoutingSetting()
	if !setting.Enabled {
		return
	}
	if !success && !channelFault {
		return // client/business error: neutral for channel health
	}
	alpha := setting.Alpha
	if alpha <= 0 || alpha > 1 {
		alpha = 0.2
	}

	s := getStat(channelID)
	s.mu.Lock()

	errSample := 0.0
	if !success {
		errSample = 1.0
	}
	if s.hasErr {
		s.errEWMA = alpha*errSample + (1-alpha)*s.errEWMA
	} else {
		s.errEWMA = errSample
		s.hasErr = true
	}

	if success && ttftMs > 0 {
		sample := float64(ttftMs)
		if s.hasTtft {
			s.ttftEWMA = alpha*sample + (1-alpha)*s.ttftEWMA
		} else {
			s.ttftEWMA = sample
			s.hasTtft = true
		}
	}

	var tripped, recovered bool
	var openUntil time.Time
	if setting.CircuitEnabled {
		if success {
			s.consecutiveFailures = 0
			if s.state != circuitClosed {
				recovered = true // half-open/open -> closed on this instance
			}
			s.state = circuitClosed
		} else {
			// channel-fault failure
			s.consecutiveFailures++
			switch s.state {
			case circuitHalfOpen:
				// a probe failed -> straight back to open
				s.trip(setting)
				tripped, openUntil = true, s.openUntil
			case circuitClosed:
				threshold := setting.OpenThreshold
				if threshold <= 0 {
					threshold = 5
				}
				if s.consecutiveFailures >= threshold {
					s.trip(setting)
					tripped, openUntil = true, s.openUntil
				}
			}
		}
	}
	s.mu.Unlock()

	// Broadcast circuit transitions to the cluster outside the lock (no-op
	// without Redis). A trip must reach other replicas so they stop selecting a
	// dead channel; a recovery clears the shared trip.
	if tripped {
		publishOpen(channelID, openUntil)
	} else if recovered {
		publishClosed(channelID)
	} else if success && setting.CircuitEnabled {
		// This replica's local circuit was already closed, but a success while
		// the channel is only half-open cluster-wide (tripped by another replica)
		// still confirms recovery — clear the shared trip so it regains full
		// weight everywhere, regardless of which replica gets the healthy traffic.
		if _, half := sharedCircuitState(channelID, time.Now().UnixNano()); half {
			publishClosed(channelID)
		}
	}
}

// Traffic carries passive, observability-only signals for one relay attempt:
// total latency, output-token throughput, and last-activity / last-error
// markers. Unlike ReportResult these never affect routing or the circuit
// breaker — they exist only to enrich the ops-monitor view — and require no
// probing (every value is a by-product of a request that already happened).
type Traffic struct {
	Success      bool
	ChannelFault bool
	LatencyMs    int64
	OutputTokens int64
	GenerationMs int64
	ErrCode      int
}

// ReportTraffic folds one attempt's observability signals into a channel's
// stats. No-op when adaptive routing is off. lastUsedAt advances on every
// attempt; latency/throughput EWMAs update on success with usable samples; the
// last channel-fault error code/time is recorded on upstream failures only, so
// a client 400 never shows up as the channel's fault.
func ReportTraffic(channelID int, tr Traffic) {
	if channelID <= 0 {
		return
	}
	setting := operation_setting.GetAdaptiveRoutingSetting()
	if !setting.Enabled {
		return
	}
	alpha := setting.Alpha
	if alpha <= 0 || alpha > 1 {
		alpha = 0.2
	}

	s := getStat(channelID)
	s.mu.Lock()
	defer s.mu.Unlock()

	nowSec := time.Now().Unix()
	s.lastUsedAt = nowSec
	s.reqWindow.add(nowSec, 1)

	if tr.Success {
		if tr.LatencyMs > 0 {
			sample := float64(tr.LatencyMs)
			if s.hasLatency {
				s.latencyEWMA = alpha*sample + (1-alpha)*s.latencyEWMA
			} else {
				s.latencyEWMA = sample
				s.hasLatency = true
			}
		}
		if tr.OutputTokens > 0 {
			s.tokWindow.add(nowSec, tr.OutputTokens)
			if tr.GenerationMs > 0 {
				sample := float64(tr.OutputTokens) / (float64(tr.GenerationMs) / 1000.0)
				if s.hasTps {
					s.tpsEWMA = alpha*sample + (1-alpha)*s.tpsEWMA
				} else {
					s.tpsEWMA = sample
					s.hasTps = true
				}
			}
		}
	} else if tr.ChannelFault {
		if tr.ErrCode > 0 {
			s.lastErrCode = tr.ErrCode
			s.lastErrAt = nowSec
		}
		// Classify every channel fault so the ops view can tell rate-limiting
		// (429) from upstream 5xx from network/other faults (code 0 included).
		switch {
		case tr.ErrCode == 429:
			s.err429++
		case tr.ErrCode >= 500:
			s.err5xx++
		default:
			s.errOther++
		}
	}
}

// trip opens the circuit; caller holds s.mu.
func (s *channelStat) trip(setting *operation_setting.AdaptiveRoutingSetting) {
	cooldown := setting.CooldownSeconds
	if cooldown <= 0 {
		cooldown = 30
	}
	s.state = circuitOpen
	s.openUntil = time.Now().Add(time.Duration(cooldown) * time.Second)
}

type snapshot struct {
	hasErr    bool
	errorRate float64
	hasTtft   bool
	ttftMs    float64
	inflight  int64
	state     circuitState

	hasLatency  bool
	latencyMs   float64
	hasTps      bool
	tps         float64
	lastUsedAt  int64
	lastErrCode int
	lastErrAt   int64
	cooldownMs  int64 // remaining until this instance's half-open probe; 0 when not open
	rpm         int64
	tpm         int64
	err429      int64
	err5xx      int64
	errOther    int64
}

// read returns the current stats, lazily transitioning open->half-open once the
// cooldown has elapsed so recovery happens from real traffic (no synthetic probe).
func (s *channelStat) read() snapshot {
	inflight := s.inflight.Load()
	now := time.Now()
	s.mu.Lock()
	if s.state == circuitOpen && now.After(s.openUntil) {
		s.state = circuitHalfOpen
	}
	var cooldownMs int64
	if s.state == circuitOpen {
		if remaining := s.openUntil.Sub(now).Milliseconds(); remaining > 0 {
			cooldownMs = remaining
		}
	}
	nowSec := now.Unix()
	snap := snapshot{
		hasErr:      s.hasErr,
		errorRate:   s.errEWMA,
		hasTtft:     s.hasTtft,
		ttftMs:      s.ttftEWMA,
		inflight:    inflight,
		state:       s.state,
		hasLatency:  s.hasLatency,
		latencyMs:   s.latencyEWMA,
		hasTps:      s.hasTps,
		tps:         s.tpsEWMA,
		lastUsedAt:  s.lastUsedAt,
		lastErrCode: s.lastErrCode,
		lastErrAt:   s.lastErrAt,
		cooldownMs:  cooldownMs,
		rpm:         s.reqWindow.sum(nowSec),
		tpm:         s.tokWindow.sum(nowSec),
		err429:      s.err429,
		err5xx:      s.err5xx,
		errOther:    s.errOther,
	}
	s.mu.Unlock()

	// Union with the cluster-wide circuit view: a channel open on any instance
	// is treated as open everywhere. Empty shared map => no change.
	if open, half := sharedCircuitState(s.channelID, time.Now().UnixNano()); open {
		snap.state = circuitOpen
	} else if half && snap.state == circuitClosed {
		snap.state = circuitHalfOpen
	}
	return snap
}

// healthMultiplier maps a channel's snapshot to a weight multiplier in
// [floor,1] and an exclusion flag. Scoring is penalize-only (never rewards a
// fast channel above baseline) to avoid a stampede onto the momentary leader.
// A channel with no data yet gets multiplier 1.0 (optimistic) so it is explored.
func healthMultiplier(snap snapshot, setting *operation_setting.AdaptiveRoutingSetting) (float64, bool) {
	h := 1.0
	if snap.hasErr {
		h -= setting.ErrorPenalty * clampFloat(snap.errorRate, 0, 1)
	}
	if snap.hasTtft && setting.TtftRefMs > 0 && snap.ttftMs > setting.TtftRefMs {
		over := (snap.ttftMs - setting.TtftRefMs) / setting.TtftRefMs
		h -= setting.TtftPenalty * clampFloat(over, 0, 1)
	}
	floor := setting.HealthFloor
	if floor < 0 {
		floor = 0
	}
	if h < floor {
		h = floor
	}
	if h > 1 {
		h = 1
	}
	if setting.CircuitEnabled {
		switch snap.state {
		case circuitOpen:
			return 0, true
		case circuitHalfOpen:
			f := setting.HalfOpenFactor
			if f <= 0 {
				f = 0.3
			}
			h *= f
		}
	}
	return h, false
}

// ShouldEscapeAffinity reports whether an affinity-anchored channel has degraded
// past the ABSOLUTE escape thresholds. Blunt thresholds are intentional: leaving
// a channel forfeits its warm upstream prompt cache, so escape only on genuinely
// bad channels, not a marginal slowdown.
func ShouldEscapeAffinity(channelID int) bool {
	setting := operation_setting.GetAdaptiveRoutingSetting()
	if !setting.Enabled || !setting.EscapeEnabled || channelID <= 0 {
		return false
	}
	v, ok := stats.Load(channelID)
	if !ok {
		// No local observation, but still honor a cluster-wide open circuit so a
		// replica that never served this channel still escapes a dead anchor.
		if setting.CircuitEnabled {
			open, _ := sharedCircuitState(channelID, time.Now().UnixNano())
			return open
		}
		return false
	}
	snap := v.(*channelStat).read() // read() already unions the shared circuit view
	if setting.CircuitEnabled && snap.state == circuitOpen {
		return true
	}
	if snap.hasErr && setting.EscapeErrorRate > 0 && snap.errorRate > setting.EscapeErrorRate {
		return true
	}
	if snap.hasTtft && setting.EscapeTtftMs > 0 && snap.ttftMs > setting.EscapeTtftMs {
		return true
	}
	return false
}

// Reset clears a channel's learned health and its circuit trip — local and
// cluster-wide — so an operator can force immediate recovery after fixing an
// upstream, instead of waiting out the cooldown. channelID <= 0 resets every
// channel.
func Reset(channelID int) {
	if channelID <= 0 {
		stats.Range(func(k, _ any) bool {
			stats.Delete(k)
			return true
		})
		clearAllShared()
		return
	}
	stats.Delete(channelID)
	clearShared(channelID)
}

// StatView is a read-only projection for observability (ops-monitor / channel badge).
type StatView struct {
	ChannelID   int     `json:"channel_id"`
	HasData     bool    `json:"has_data"`
	ErrorRate   float64 `json:"error_rate"`
	HasTtft     bool    `json:"has_ttft"`
	TtftMs      float64 `json:"ttft_ms"`
	Inflight    int64   `json:"inflight"`
	CircuitOpen bool    `json:"circuit_open"`
	CircuitHalf bool    `json:"circuit_half_open"`

	// Observability-only enrichments (see Traffic / ReportTraffic).
	HasLatency  bool    `json:"has_latency"`
	LatencyMs   float64 `json:"latency_ms"`
	HasTps      bool    `json:"has_tps"`
	Tps         float64 `json:"tps"`           // output tokens/sec
	LastUsedAt  int64   `json:"last_used_at"`  // unix seconds; 0 = never
	LastErrCode int     `json:"last_err_code"` // last channel-fault HTTP status; 0 = none
	LastErrAt   int64   `json:"last_err_at"`   // unix seconds; 0 = none
	CooldownMs  int64   `json:"cooldown_ms"`   // remaining until half-open; 0 unless open
	Rpm         int64   `json:"rpm"`           // requests in the last 60s
	Tpm         int64   `json:"tpm"`           // output tokens in the last 60s
	Err429      int64   `json:"err_429"`       // cumulative rate-limit faults
	Err5xx      int64   `json:"err_5xx"`       // cumulative upstream 5xx faults
	ErrOther    int64   `json:"err_other"`     // cumulative other channel faults (incl. network)
	Weight      float64 `json:"weight"`        // current health-derived routing multiplier, [0,1]
}

func viewFromSnapshot(channelID int, snap snapshot) StatView {
	// The health multiplier is what routing would currently apply to this
	// channel (penalize-only, [floor,1], ×halfOpenFactor when half-open, 0 when
	// open) — surfaced so an operator can see why a channel gets more/less
	// traffic, not just its raw stats.
	weight, _ := healthMultiplier(snap, operation_setting.GetAdaptiveRoutingSetting())
	return StatView{
		ChannelID:   channelID,
		HasData:     snap.hasErr || snap.hasTtft,
		ErrorRate:   snap.errorRate,
		HasTtft:     snap.hasTtft,
		TtftMs:      snap.ttftMs,
		Inflight:    snap.inflight,
		CircuitOpen: snap.state == circuitOpen,
		CircuitHalf: snap.state == circuitHalfOpen,
		HasLatency:  snap.hasLatency,
		LatencyMs:   snap.latencyMs,
		HasTps:      snap.hasTps,
		Tps:         snap.tps,
		LastUsedAt:  snap.lastUsedAt,
		LastErrCode: snap.lastErrCode,
		LastErrAt:   snap.lastErrAt,
		CooldownMs:  snap.cooldownMs,
		Rpm:         snap.rpm,
		Tpm:         snap.tpm,
		Err429:      snap.err429,
		Err5xx:      snap.err5xx,
		ErrOther:    snap.errOther,
		Weight:      weight,
	}
}

// GetStatView returns a channel's current health for display, or ok=false if the
// channel has never been observed.
func GetStatView(channelID int) (StatView, bool) {
	v, ok := stats.Load(channelID)
	if !ok {
		return StatView{ChannelID: channelID}, false
	}
	return viewFromSnapshot(channelID, v.(*channelStat).read()), true
}

// AllStatViews returns the current health of every observed channel, for the
// admin observability endpoint.
func AllStatViews() []StatView {
	views := make([]StatView, 0)
	stats.Range(func(k, v any) bool {
		id, _ := k.(int)
		s, _ := v.(*channelStat)
		if s != nil {
			views = append(views, viewFromSnapshot(id, s.read()))
		}
		return true
	})
	return views
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
