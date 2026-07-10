package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// AdaptiveRoutingSetting controls passive, self-adaptive channel routing.
//
// Design (validated against sub2api / AxonHub reference gateways):
//   - Fully passive: every signal comes from real relay traffic (EWMA of
//     TTFT + channel-fault error rate). No active probing, so it never spends
//     upstream quota just to measure health.
//   - Affinity-first: session/prompt-cache affinity stays authoritative; the
//     adaptive layer only decides the cold-start pick (no anchor yet) and
//     re-weights candidates within a priority layer. Escape from an anchored
//     channel happens only on ABSOLUTE health thresholds so a warm prompt
//     cache is not thrown away for a marginal latency difference.
//   - Penalize-only scoring (never reward) to avoid a Matthew-effect stampede
//     onto whichever channel is momentarily fastest.
//   - Passive circuit breaker: consecutive channel-fault failures open a
//     channel; after a cooldown it half-opens and recovers from real traffic
//     (reduced weight), no synthetic probe.
type AdaptiveRoutingSetting struct {
	Enabled bool `json:"enabled"`

	// Alpha is the EWMA smoothing factor for TTFT / error-rate samples.
	Alpha float64 `json:"alpha"`

	// TtftRefMs is the reference first-token latency (ms). A channel is only
	// penalized for TTFT once its EWMA exceeds this reference.
	TtftRefMs float64 `json:"ttft_ref_ms"`
	// TtftPenalty is the maximum score reduction from slow TTFT (0..1).
	TtftPenalty float64 `json:"ttft_penalty"`
	// ErrorPenalty is the score reduction per unit error-rate EWMA (0..1).
	ErrorPenalty float64 `json:"error_penalty"`
	// HealthFloor is the minimum health multiplier, so a degraded channel is
	// de-weighted but never fully starved (keeps passive recovery traffic).
	HealthFloor float64 `json:"health_floor"`
	// InflightPenalty divides a candidate's weight by (1 + InflightPenalty *
	// in-flight requests). This is the "peak" in Peak-EWMA: a channel that is
	// both slow and busy is avoided faster than latency alone would. 0 disables
	// load-awareness. Counts are per-instance (this replica's own in-flight).
	InflightPenalty float64 `json:"inflight_penalty"`
	// TopK, when > 0, keeps only the top-K candidates by effective weight
	// before the weighted-random pick. 0 = keep all (fine at small fan-out).
	TopK int `json:"top_k"`

	// CircuitEnabled toggles the passive circuit breaker.
	CircuitEnabled bool `json:"circuit_enabled"`
	// OpenThreshold is the number of consecutive channel-fault failures that
	// trips a channel from closed to open.
	OpenThreshold int `json:"open_threshold"`
	// CooldownSeconds is how long a channel stays open before half-opening.
	CooldownSeconds int `json:"cooldown_seconds"`
	// HalfOpenFactor scales a half-open channel's weight so only a trickle of
	// real traffic probes it (AxonHub uses 0.3).
	HalfOpenFactor float64 `json:"half_open_factor"`

	// EscapeEnabled toggles affinity escape on health degradation. When off,
	// affinity only escapes on hard channel disable (legacy behavior).
	EscapeEnabled bool `json:"escape_enabled"`
	// EscapeTtftMs / EscapeErrorRate are ABSOLUTE thresholds. The anchored
	// channel is abandoned only when genuinely bad, preserving prompt cache.
	EscapeTtftMs    float64 `json:"escape_ttft_ms"`
	EscapeErrorRate float64 `json:"escape_error_rate"`
}

// adaptiveRoutingSetting: fork 默认开启自适应路由（上游为 opt-in）。
// 依赖内存渠道缓存(MemoryCacheEnabled)生效，直查数据库路径不参与。
// OpenThreshold=3：客户端错误不计入连败，3 次连续渠道故障已高度可信，
// 更快切断坏渠道；半开(0.3 权重)+30s 冷却让恢复代价很低。
// 逃逸阈值保持钝化(8s/50%)，避免为边际延迟差抛弃温热的提示词缓存。
var adaptiveRoutingSetting = AdaptiveRoutingSetting{
	Enabled: true,

	Alpha: 0.2,

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

func init() {
	config.GlobalConfig.Register("adaptive_routing_setting", &adaptiveRoutingSetting)
}

func GetAdaptiveRoutingSetting() *AdaptiveRoutingSetting {
	return &adaptiveRoutingSetting
}
