package keeper

import sdkmath "cosmossdk.io/math"

// slidingWindowAtLimit reports whether the sliding-window count
//
//	cur + prev * (window - elapsed) / window
//
// has reached limit. It is evaluated exactly in integer arithmetic, as
// cur*window + prev*(window-elapsed) >= limit*window, so every validator
// reaches the same answer: a float64 version can be fused into an FMA on
// some architectures (arm64) and round differently at the boundary.
func slidingWindowAtLimit(cur, prev uint64, elapsed, window int64, limit uint64) bool {
	if window <= 0 {
		return sdkmath.NewIntFromUint64(cur).GTE(sdkmath.NewIntFromUint64(limit))
	}
	if elapsed < 0 {
		elapsed = 0
	}
	remaining := int64(0)
	if elapsed < window {
		remaining = window - elapsed
	}
	lhs := sdkmath.NewIntFromUint64(cur).MulRaw(window).
		Add(sdkmath.NewIntFromUint64(prev).MulRaw(remaining))
	rhs := sdkmath.NewIntFromUint64(limit).MulRaw(window)
	return lhs.GTE(rhs)
}
