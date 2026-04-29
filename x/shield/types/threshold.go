package types

// ComputeThreshold returns ceil(numerator * total / denominator), with a
// minimum of 1 when the result would otherwise be zero for a non-empty set.
// If denominator is zero the function returns total (degenerate "all
// validators required" fallback). If total is zero the function returns 0.
//
// This is the single source of truth for DKG/TLE threshold computation,
// shared by ABCI handlers and the keeper to avoid floor/ceil drift.
func ComputeThreshold(numerator, denominator, total uint64) uint64 {
	if total == 0 {
		return 0
	}
	if denominator == 0 {
		return total
	}
	t := (numerator*total + denominator - 1) / denominator
	if t == 0 {
		t = 1
	}
	return t
}
