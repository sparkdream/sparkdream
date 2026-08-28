//go:build testnet

package types

import (
	"cosmossdk.io/math"
)

// Testnet values — 2x devnet thresholds, approaching production difficulty.
// Build with: go build -tags testnet
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(50),   // production: 50
		ProvisionalMinInterims:       3,                       // production: 3
		EstablishedMinRep:            math.LegacyNewDec(200),  // production: 200
		EstablishedMinInterims:       10,                      // production: 10
		TrustedMinRep:                math.LegacyNewDec(500),  // production: 500
		TrustedMinSeasons:            2,                       // production: 1
		CoreMinRep:                   math.LegacyNewDec(1000), // production: 1000
		CoreMinSeasons:               2,                       // production: 2
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 4,  // production: 1
		EstablishedInvitationCredits: 8,  // production: 3
		TrustedInvitationCredits:     14, // production: 5
		CoreInvitationCredits:        30, // production: 10
	}
}

// getSentinelRewardEpochBlocks returns the cadence at which the sentinel SPARK
// reward pool is drained on testnet (~1/2 day at 6s blocks).
func getSentinelRewardEpochBlocks() uint64 {
	return 7200
}

// getVerifierRewardEpochBlocks returns the federation-verifier reward
// cadence. Deliberately its own dial rather than the sentinel's: a
// verification is challengeable for far longer than a hide is appealable, so
// the epoch has to be long enough that a challenge filed against work in it
// can plausibly resolve before the accuracy window scores that work.
//
// Concretely, that means ONE FULL federation challenge_window, expressed in
// blocks at 6s. An epoch shorter than the challenge window scores a
// verifier's accuracy before the challenges against that epoch's work can
// resolve, so the window is always reading a stale verdict count.
func getVerifierRewardEpochBlocks() uint64 {
	return 43200 // 3d challenge_window
}

// getMinEpochVerifications is the per-epoch verification floor a bonded
// federation verifier must clear to be eligible for pay.
func getMinEpochVerifications() uint32 {
	return 3
}

// getMinVerifierAccuracy is the windowed-accuracy bar for verifier pay,
// matching federation's pre-migration 0.8.
func getMinVerifierAccuracy() math.LegacyDec {
	return math.LegacyNewDecWithPrec(80, 2)
}

// getMaxVerifierDreamMintPerEpoch caps the total DREAM stipend minted across
// all eligible verifiers in one reward epoch.
func getMaxVerifierDreamMintPerEpoch() math.Int {
	return math.NewInt(100000000) // 100 DREAM
}

// getInvitationCostMultiplier returns 1.1x per-invitation escalation
// matching production.
func getInvitationCostMultiplier() math.LegacyDec {
	return math.LegacyNewDecWithPrec(110, 2) // 1.1x
}
