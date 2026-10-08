package ante

import "time"

// SetProofGuardForTest overrides the proof guard's tuning for decorators
// built afterwards and returns a func restoring the previous values.
func SetProofGuardForTest(cacheSize int, rate, burst float64, now func() time.Time) (restore func()) {
	oldSize, oldRate, oldBurst, oldNow := rejectedProofCacheSize, proofVerifyRate, proofVerifyBurst, nowFunc
	rejectedProofCacheSize, proofVerifyRate, proofVerifyBurst, nowFunc = cacheSize, rate, burst, now
	return func() {
		rejectedProofCacheSize, proofVerifyRate, proofVerifyBurst, nowFunc = oldSize, oldRate, oldBurst, oldNow
	}
}

// RejectedProofCacheLen reports how many rejections d's cache holds.
func RejectedProofCacheLen(d ShieldGasDecorator) int { return d.guard.rejected.len() }

// ShieldCircuitID is the VK circuit id the guard reads.
const ShieldCircuitID = shieldCircuitID
