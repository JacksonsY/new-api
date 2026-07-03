package billingexpr

import "math"

// QuotaRound converts a float64 quota value to int using half-away-from-zero
// rounding. Every tiered billing path (pre-consume, settlement, breakdown
// validation, log fields) MUST use this function to avoid +-1 discrepancies.
//
// NaN/Inf 与超出 int 范围的值会被钳制，避免 int(NaN)/int(Inf) 的未定义行为。
// 正常路径下 runProgram 已拒绝 NaN/Inf/负值，这里是最后一道防线。
func QuotaRound(f float64) int {
	if math.IsNaN(f) {
		return 0
	}
	r := math.Round(f)
	if r >= float64(math.MaxInt) {
		return math.MaxInt
	}
	if r <= float64(math.MinInt) {
		return math.MinInt
	}
	return int(r)
}
