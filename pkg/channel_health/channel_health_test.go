package channelhealth

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetHealth clears all in-memory channel stats and the shared circuit
// snapshot between tests.
func resetHealth() {
	stats.Range(func(k, _ any) bool {
		stats.Delete(k)
		return true
	})
	sharedCircuits.Store(map[int]int64{})
}

// configureAdaptive installs a deterministic adaptive-routing config for tests
// and returns the live pointer so a test can tweak individual knobs.
func configureAdaptive(t *testing.T) *operation_setting.AdaptiveRoutingSetting {
	t.Helper()
	resetHealth()
	s := operation_setting.GetAdaptiveRoutingSetting()
	*s = operation_setting.AdaptiveRoutingSetting{
		Enabled:         true,
		Alpha:           0.2,
		TtftRefMs:       2000,
		TtftPenalty:     0.5,
		ErrorPenalty:    0.8,
		HealthFloor:     0.05,
		InflightPenalty: 0.5,
		TopK:            0,
		CircuitEnabled:  true,
		OpenThreshold:   3,
		CooldownSeconds: 30,
		HalfOpenFactor:  0.3,
		EscapeEnabled:   true,
		EscapeTtftMs:    8000,
		EscapeErrorRate: 0.5,
	}
	return s
}

func TestReportResultEWMA(t *testing.T) {
	configureAdaptive(t)

	// first sample seeds the EWMA directly
	ReportResult(1, true, false, 1000)
	view, ok := GetStatView(1)
	require.True(t, ok)
	assert.InDelta(t, 1000, view.TtftMs, 0.001)
	assert.InDelta(t, 0, view.ErrorRate, 0.001)

	// second TTFT sample: 0.2*2000 + 0.8*1000 = 1200
	ReportResult(1, true, false, 2000)
	view, _ = GetStatView(1)
	assert.InDelta(t, 1200, view.TtftMs, 0.001)

	// a channel-fault failure lifts the error EWMA: 0.2*1 + 0.8*0 = 0.2
	ReportResult(1, false, true, 0)
	view, _ = GetStatView(1)
	assert.InDelta(t, 0.2, view.ErrorRate, 0.001)
}

func TestClientErrorIsNeutral(t *testing.T) {
	configureAdaptive(t)

	ReportResult(1, true, false, 1500) // establish baseline
	before, _ := GetStatView(1)

	// client/business error (channelFault=false) must not touch health
	ReportResult(1, false, false, 0)
	after, ok := GetStatView(1)
	require.True(t, ok)
	assert.Equal(t, before.ErrorRate, after.ErrorRate, "client error must not raise error rate")
	assert.Equal(t, before.TtftMs, after.TtftMs)
}

func TestCircuitBreakerLifecycle(t *testing.T) {
	s := configureAdaptive(t)
	require.Equal(t, 3, s.OpenThreshold)

	// below threshold: still closed
	ReportResult(7, false, true, 0)
	ReportResult(7, false, true, 0)
	view, _ := GetStatView(7)
	assert.False(t, view.CircuitOpen, "2 < threshold(3) should not open")

	// third consecutive channel-fault trips the breaker
	ReportResult(7, false, true, 0)
	view, _ = GetStatView(7)
	assert.True(t, view.CircuitOpen, "3rd consecutive fault should open circuit")

	// while open the channel is excluded from selection (2-candidate layer, since
	// a single candidate short-circuits Select regardless of health)
	id, ok := Select([]Candidate{{ChannelID: 7, Weight: 100}, {ChannelID: 8, Weight: 100}})
	require.True(t, ok)
	assert.Equal(t, 8, id, "open channel must not be selected while a healthy one exists")

	// force cooldown to elapse -> lazy transition to half-open on next read
	st := getStat(7)
	st.mu.Lock()
	st.openUntil = time.Now().Add(-time.Second)
	st.mu.Unlock()
	view, _ = GetStatView(7)
	assert.True(t, view.CircuitHalf, "after cooldown the circuit should be half-open")
	assert.False(t, view.CircuitOpen)

	// a real success in half-open closes it
	ReportResult(7, true, false, 500)
	view, _ = GetStatView(7)
	assert.False(t, view.CircuitOpen)
	assert.False(t, view.CircuitHalf, "success in half-open should close the circuit")
}

func TestHalfOpenReopensOnFailure(t *testing.T) {
	configureAdaptive(t)
	// open it
	for i := 0; i < 3; i++ {
		ReportResult(5, false, true, 0)
	}
	require.True(t, mustView(t, 5).CircuitOpen)

	// half-open via elapsed cooldown
	st := getStat(5)
	st.mu.Lock()
	st.openUntil = time.Now().Add(-time.Second)
	st.mu.Unlock()
	require.True(t, mustView(t, 5).CircuitHalf)

	// a failed probe returns straight to open
	ReportResult(5, false, true, 0)
	assert.True(t, mustView(t, 5).CircuitOpen, "failed probe should re-open the circuit")
}

func TestSelectFailOpenWhenAllExcluded(t *testing.T) {
	configureAdaptive(t)
	for _, id := range []int{1, 2} {
		for i := 0; i < 3; i++ {
			ReportResult(id, false, true, 0)
		}
	}
	// both circuits open -> Select must yield ok=false so the caller falls back
	// to legacy weighted-random rather than stranding the request.
	_, ok := Select([]Candidate{{ChannelID: 1, Weight: 100}, {ChannelID: 2, Weight: 100}})
	assert.False(t, ok, "all-excluded must fail open to legacy selection")
}

func TestSelectBootstrapOptimistic(t *testing.T) {
	configureAdaptive(t)
	// brand-new channels with no data should be selectable (optimistic weight),
	// so a freshly added channel is explored rather than starved.
	seen := map[int]int{}
	for i := 0; i < 200; i++ {
		id, ok := Select([]Candidate{{ChannelID: 10, Weight: 100}, {ChannelID: 11, Weight: 100}})
		require.True(t, ok)
		seen[id]++
	}
	assert.Positive(t, seen[10], "new channel 10 should receive exploration traffic")
	assert.Positive(t, seen[11], "new channel 11 should receive exploration traffic")
}

func TestSelectAvoidsDegradedChannel(t *testing.T) {
	configureAdaptive(t)
	// channel 20 healthy & fast; channel 21 slow + erroring but circuit still closed.
	for i := 0; i < 20; i++ {
		ReportResult(20, true, false, 300)
	}
	// push 21's TTFT well past the reference and error rate up, without tripping
	// the breaker (interleave successes so consecutive-fault count stays low).
	for i := 0; i < 20; i++ {
		ReportResult(21, true, false, 6000)
		ReportResult(21, false, true, 0)
		ReportResult(21, true, false, 6000)
	}
	require.False(t, mustView(t, 21).CircuitOpen)

	seen := map[int]int{}
	for i := 0; i < 400; i++ {
		id, ok := Select([]Candidate{{ChannelID: 20, Weight: 100}, {ChannelID: 21, Weight: 100}})
		require.True(t, ok)
		seen[id]++
	}
	// equal static weight, but health penalty must skew traffic to the fast one.
	assert.Greater(t, seen[20], seen[21], "degraded channel should get less traffic at equal weight")
	assert.Positive(t, seen[21], "degraded channel still gets floor traffic (passive recovery)")
}

func TestSelectInflightPenalty(t *testing.T) {
	configureAdaptive(t)
	// two channels, equal health/weight; load channel 31 with in-flight requests.
	for i := 0; i < 8; i++ {
		AcquireInflight(31)
	}
	view, ok := GetStatView(31)
	require.True(t, ok)
	assert.EqualValues(t, 8, view.Inflight)

	seen := map[int]int{}
	for i := 0; i < 400; i++ {
		id, ok := Select([]Candidate{{ChannelID: 30, Weight: 100}, {ChannelID: 31, Weight: 100}})
		require.True(t, ok)
		seen[id]++
	}
	assert.Greater(t, seen[30], seen[31], "busy channel should get less traffic (peak weighting)")

	// releasing all in-flight clamps the counter at zero (never negative)
	for i := 0; i < 10; i++ {
		ReleaseInflight(31)
	}
	view, _ = GetStatView(31)
	assert.EqualValues(t, 0, view.Inflight)
}

func TestShouldEscapeAffinity(t *testing.T) {
	s := configureAdaptive(t)

	// no data -> no escape
	assert.False(t, ShouldEscapeAffinity(1))

	// healthy -> no escape
	ReportResult(1, true, false, 500)
	assert.False(t, ShouldEscapeAffinity(1))

	// slow TTFT beyond absolute threshold -> escape
	resetHealth()
	for i := 0; i < 30; i++ {
		ReportResult(2, true, false, 12000)
	}
	assert.True(t, ShouldEscapeAffinity(2), "TTFT past EscapeTtftMs should escape")

	// high error rate beyond absolute threshold -> escape
	resetHealth()
	for i := 0; i < 10; i++ {
		ReportResult(3, false, true, 0)
	}
	assert.True(t, ShouldEscapeAffinity(3), "error rate past EscapeErrorRate should escape")

	// disabling the feature suppresses escape entirely
	s.Enabled = false
	assert.False(t, ShouldEscapeAffinity(3))
}

func TestSharedCircuitExcludesAcrossInstances(t *testing.T) {
	configureAdaptive(t)
	// Simulate another instance having tripped channel 41 (openUntil in future).
	sharedCircuits.Store(map[int]int64{41: time.Now().Add(time.Minute).UnixMilli()})
	// This replica never observed 41 locally, yet must exclude it cluster-wide.
	for i := 0; i < 50; i++ {
		id, ok := Select([]Candidate{{ChannelID: 40, Weight: 100}, {ChannelID: 41, Weight: 100}})
		require.True(t, ok)
		assert.Equal(t, 40, id, "channel open on another instance must be excluded")
	}
}

func TestSharedCircuitEscapeWithoutLocalStat(t *testing.T) {
	configureAdaptive(t)
	sharedCircuits.Store(map[int]int64{42: time.Now().Add(time.Minute).UnixMilli()})
	// No local stat for 42, but the cluster says open -> escape the anchor.
	assert.True(t, ShouldEscapeAffinity(42))
	// Unknown, unobserved channel does not escape.
	assert.False(t, ShouldEscapeAffinity(99))
}

func TestSharedCircuitMergedIntoRead(t *testing.T) {
	configureAdaptive(t)

	// Future openUntil => cluster-open, merged into the local read.
	sharedCircuits.Store(map[int]int64{43: time.Now().Add(time.Minute).UnixMilli()})
	assert.Equal(t, circuitOpen, getStat(43).read().state)

	// Past openUntil => cluster half-open (recovering), not full exclusion.
	sharedCircuits.Store(map[int]int64{43: time.Now().Add(-time.Second).UnixMilli()})
	assert.Equal(t, circuitHalfOpen, getStat(43).read().state)
}

func TestResetClearsHealthAndCircuit(t *testing.T) {
	configureAdaptive(t)

	// open channel 60 locally + a simulated shared trip
	for i := 0; i < 3; i++ {
		ReportResult(60, false, true, 0)
	}
	require.True(t, mustView(t, 60).CircuitOpen)
	sharedCircuits.Store(map[int]int64{60: time.Now().Add(time.Minute).UnixMilli()})

	Reset(60)

	_, ok := GetStatView(60)
	assert.False(t, ok, "reset should drop the local stat")
	open, half := sharedCircuitState(60, time.Now().UnixMilli())
	assert.False(t, open, "reset should clear the shared trip")
	assert.False(t, half)

	// reset-all wipes every channel + the whole shared map
	for i := 0; i < 3; i++ {
		ReportResult(61, false, true, 0)
	}
	sharedCircuits.Store(map[int]int64{62: time.Now().Add(time.Minute).UnixMilli()})
	Reset(0)
	assert.Empty(t, AllStatViews(), "reset-all should drop every local stat")
	o, h := sharedCircuitState(62, time.Now().UnixMilli())
	assert.False(t, o)
	assert.False(t, h)
}

func mustView(t *testing.T, id int) StatView {
	t.Helper()
	v, ok := GetStatView(id)
	require.True(t, ok)
	return v
}

func TestReportTrafficThroughputAndLatency(t *testing.T) {
	configureAdaptive(t)

	// First success seeds the EWMAs directly: 100 tokens over 2000ms = 50 tok/s.
	ReportTraffic(1, Traffic{Success: true, LatencyMs: 3000, OutputTokens: 100, GenerationMs: 2000})
	view := mustView(t, 1)
	assert.True(t, view.HasLatency)
	assert.InDelta(t, 3000, view.LatencyMs, 0.001)
	assert.True(t, view.HasTps)
	assert.InDelta(t, 50, view.Tps, 0.001)
	assert.NotZero(t, view.LastUsedAt)

	// Second success (alpha=0.2): latency 0.2*1000+0.8*3000=2600;
	// tps sample 200/2s=100 -> 0.2*100+0.8*50=60.
	ReportTraffic(1, Traffic{Success: true, LatencyMs: 1000, OutputTokens: 200, GenerationMs: 2000})
	view = mustView(t, 1)
	assert.InDelta(t, 2600, view.LatencyMs, 0.001)
	assert.InDelta(t, 60, view.Tps, 0.001)
}

func TestReportTrafficLastErrorGating(t *testing.T) {
	configureAdaptive(t)

	// A channel-fault failure records the last upstream error code + time.
	ReportTraffic(1, Traffic{Success: false, ChannelFault: true, ErrCode: 502})
	view := mustView(t, 1)
	assert.Equal(t, 502, view.LastErrCode)
	assert.NotZero(t, view.LastErrAt)

	// A client fault (4xx, not the channel's fault) must not overwrite it.
	ReportTraffic(1, Traffic{Success: false, ChannelFault: false, ErrCode: 400})
	view = mustView(t, 1)
	assert.Equal(t, 502, view.LastErrCode, "client fault must not overwrite last channel error")
}

func TestReportTrafficDisabledIsNoop(t *testing.T) {
	configureAdaptive(t)
	operation_setting.GetAdaptiveRoutingSetting().Enabled = false
	ReportTraffic(1, Traffic{Success: true, LatencyMs: 1000, OutputTokens: 100, GenerationMs: 1000})
	_, ok := GetStatView(1)
	assert.False(t, ok, "no stats recorded while adaptive routing is off")
}

func TestCooldownRemainingExposed(t *testing.T) {
	s := configureAdaptive(t)
	s.OpenThreshold = 1
	s.CooldownSeconds = 30

	// One channel-fault failure trips the circuit (threshold 1).
	ReportResult(1, false, true, 0)
	view := mustView(t, 1)
	require.True(t, view.CircuitOpen)
	assert.Greater(t, view.CooldownMs, int64(0))
	assert.LessOrEqual(t, view.CooldownMs, int64(30_000))
}

func TestRollingWindowAndErrorClasses(t *testing.T) {
	configureAdaptive(t)

	// Five successes (10 output tokens each) + three channel faults of distinct
	// classes, all within the same rolling window.
	for i := 0; i < 5; i++ {
		ReportTraffic(1, Traffic{Success: true, LatencyMs: 100, OutputTokens: 10, GenerationMs: 100})
	}
	ReportTraffic(1, Traffic{Success: false, ChannelFault: true, ErrCode: 429})
	ReportTraffic(1, Traffic{Success: false, ChannelFault: true, ErrCode: 503})
	ReportTraffic(1, Traffic{Success: false, ChannelFault: true, ErrCode: 0}) // network -> other

	view := mustView(t, 1)
	assert.Equal(t, int64(8), view.Rpm, "every attempt counts toward RPM")
	assert.Equal(t, int64(50), view.Tpm, "5 successes * 10 output tokens")
	assert.Equal(t, int64(1), view.Err429)
	assert.Equal(t, int64(1), view.Err5xx)
	assert.Equal(t, int64(1), view.ErrOther)
}

func TestWeightReflectsHealth(t *testing.T) {
	s := configureAdaptive(t)

	// Healthy + fast (ttft below the reference) => full weight 1.0.
	ReportResult(1, true, false, 500)
	assert.InDelta(t, 1.0, mustView(t, 1).Weight, 0.001)

	// A tripped circuit excludes the channel => weight 0.
	s.OpenThreshold = 1
	ReportResult(2, false, true, 0)
	v := mustView(t, 2)
	require.True(t, v.CircuitOpen)
	assert.InDelta(t, 0, v.Weight, 0.001)
}
