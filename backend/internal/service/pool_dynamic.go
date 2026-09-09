package service

import (
	"math"
	"sort"
)

// DynamicFairAllocation 以相同累计使用水位分配新增份额。capacity 是总容量，
// loads 是每人已用及预占；保留小数并向下取整，不回收已消耗份额。
func DynamicFairAllocation(capacity float64, loads []float64) []float64 {
	out := make([]float64, len(loads))
	if capacity <= 0 || len(loads) == 0 {
		return out
	}
	used := 0.0
	for _, v := range loads {
		if v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			used += v
		}
	}
	remaining := capacity - used
	if remaining <= 0 {
		return out
	}
	// Water filling is small (at most 50 seats), so a sorted level scan is
	// clearer than an allocation tree.  Decimal points are retained in the
	// ledger; presentation may round them for compactness.
	level := make([]float64, len(loads))
	copy(level, loads)
	for i := range level {
		if level[i] < 0 || math.IsNaN(level[i]) || math.IsInf(level[i], 0) {
			level[i] = 0
		}
	}
	unique := append([]float64(nil), level...)
	sort.Float64s(unique)
	compact := unique[:0]
	for _, v := range unique {
		if len(compact) == 0 || math.Abs(compact[len(compact)-1]-v) > 1e-9 {
			compact = append(compact, v)
		}
	}
	water := compact[0]
	for i, next := range compact[1:] {
		count := 0
		for _, v := range level {
			if v <= water+1e-9 {
				count++
			}
		}
		cost := (next - water) * float64(count)
		if remaining <= cost {
			water += remaining / float64(count)
			remaining = 0
			break
		}
		remaining -= cost
		water = next
		if i == len(compact)-2 {
			count = len(level)
			if count > 0 && remaining > 0 {
				water += remaining / float64(count)
				remaining = 0
			}
		}
	}
	if remaining > 0 && len(compact) == 1 {
		water += remaining / float64(len(level))
	}
	for i, v := range level {
		out[i] = math.Max(0, water-v)
	}
	for i := range out {
		if out[i] < 0 || math.IsNaN(out[i]) || math.IsInf(out[i], 0) {
			out[i] = 0
		}
		out[i] = math.Floor(out[i]*1e8) / 1e8
	}
	return out
}

// DynamicCreditPercentage is the conservative initial estimator required by
// the dynamic protocol.  Standard API dollars are a weighting feature only;
// they do not become a legacy dollar limit.
func DynamicCreditPercentage(credit float64) float64 {
	if credit <= 0 || math.IsNaN(credit) || math.IsInf(credit, 0) {
		return 0
	}
	return math.Max(1, credit*10)
}

// DynamicCalibratedCreditPercentage uses an observed pp/$ ratio after at
// least one attributable sample.  A cold account keeps the conservative 1pp
// floor; calibrated requests retain four decimal places and round upward so
// a reservation cannot understate a fractional percentage.
func DynamicCalibratedCreditPercentage(credit, ratio float64, samples int64) float64 {
	if credit <= 0 || math.IsNaN(credit) || math.IsInf(credit, 0) {
		return 0
	}
	if samples <= 0 || ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return DynamicCreditPercentage(credit)
	}
	return math.Max(0.0001, math.Ceil(credit*ratio*10000)/10000)
}
