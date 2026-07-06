package channelhealth

import (
	"math/rand"
	"sort"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// Candidate is one channel in a single priority layer, with its admin-configured
// weight. Passed by the router; channelhealth stays free of the model package.
type Candidate struct {
	ChannelID int
	Weight    int
}

// Select re-ranks a priority layer's candidates by passive health and returns
// the chosen channel ID.
//
// ok=false tells the caller to fall back to its legacy weighted-random pick —
// this happens when adaptive routing is disabled, the input is degenerate, or
// the circuit breaker has excluded every candidate (fail-open: never strand a
// request just because all breakers look tripped).
//
// At full health the effective weights equal legacy's smoothed weights, so
// turning the feature on is a no-op until real degradation is observed.
func Select(candidates []Candidate) (int, bool) {
	setting := operation_setting.GetAdaptiveRoutingSetting()
	if !setting.Enabled || len(candidates) == 0 {
		return 0, false
	}
	if len(candidates) == 1 {
		return candidates[0].ChannelID, true
	}

	base := smoothedBaseWeights(candidates)

	type scored struct {
		id     int
		weight float64
	}
	active := make([]scored, 0, len(candidates))
	for i, c := range candidates {
		snap := getStat(c.ChannelID).read()
		mult, excluded := healthMultiplier(snap, setting)
		if excluded {
			continue
		}
		w := base[i] * mult
		// Peak weighting: de-weight a channel that already has requests in
		// flight, so a fast-but-swamped channel is avoided before it slows down.
		if setting.InflightPenalty > 0 && snap.inflight > 0 {
			w /= 1 + setting.InflightPenalty*float64(snap.inflight)
		}
		if w < 0 {
			w = 0
		}
		active = append(active, scored{id: c.ChannelID, weight: w})
	}

	if len(active) == 0 {
		return 0, false // fail-open to legacy selection
	}

	if setting.TopK > 0 && len(active) > setting.TopK {
		sort.Slice(active, func(i, j int) bool { return active[i].weight > active[j].weight })
		active = active[:setting.TopK]
	}

	total := 0.0
	for _, a := range active {
		total += a.weight
	}
	if total <= 0 {
		return active[rand.Intn(len(active))].id, true
	}
	r := rand.Float64() * total
	acc := 0.0
	for _, a := range active {
		acc += a.weight
		if r <= acc {
			return a.id, true
		}
	}
	return active[len(active)-1].id, true
}

// smoothedBaseWeights mirrors GetRandomSatisfiedChannel's smoothing so that at
// full health the adaptive distribution is identical to legacy behavior.
func smoothedBaseWeights(candidates []Candidate) []float64 {
	sumWeight := 0
	for _, c := range candidates {
		sumWeight += c.Weight
	}
	smoothingFactor := 1
	smoothingAdjustment := 0
	if sumWeight == 0 {
		smoothingAdjustment = 100
	} else if sumWeight/len(candidates) < 10 {
		smoothingFactor = 100
	}
	out := make([]float64, len(candidates))
	for i, c := range candidates {
		out[i] = float64(c.Weight*smoothingFactor + smoothingAdjustment)
	}
	return out
}
