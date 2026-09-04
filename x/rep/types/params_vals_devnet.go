//go:build devnet

package types

import (
	"cosmossdk.io/math"
)

// Devnet values — most permissive of the three networks for fast iteration.
// Trust thresholds sit below testnet/mainnet; invitation credits sit above
// them so dev work never bumps into invite caps.
// Build with: go build -tags devnet
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(25),  // production: 50
		ProvisionalMinInterims:       2,                      // production: 3
		EstablishedMinRep:            math.LegacyNewDec(100), // production: 200
		EstablishedMinInterims:       5,                      // production: 10
		TrustedMinRep:                math.LegacyNewDec(250), // production: 500
		TrustedMinSeasons:            1,                      // production: 1
		CoreMinRep:                   math.LegacyNewDec(500), // production: 1000
		CoreMinSeasons:               1,                      // production: 2
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 5,  // mainnet: 3, testnet: 4
		EstablishedInvitationCredits: 10, // mainnet: 6, testnet: 8
		TrustedInvitationCredits:     18, // mainnet: 10, testnet: 14
		CoreInvitationCredits:        40, // mainnet: 20, testnet: 30
	}
}

// getSentinelRewardEpochBlocks returns the cadence at which the sentinel SPARK
// reward pool is drained on devnet (~6h at 6s blocks — fastest of the three
// networks so devs can observe a full epoch in a single working session).
func getSentinelRewardEpochBlocks() uint64 {
	return 3600
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
	return 1200 // 2h challenge_window
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
// matching production. Devnet keeps the same anti-sybil mechanic so dev
// builds exercise the same code path as mainnet.
func getInvitationCostMultiplier() math.LegacyDec {
	return math.LegacyNewDecWithPrec(110, 2) // 1.1x
}

// getMaxInterimRewardsPerSeason is the per-season ceiling on DREAM minted to
// pay interim work. Interims are self-assigned by their creator and
// self-completed by their assignee, so max_active_interims_per_member bounds
// only how many are open at once, not how many a member can complete and
// re-create in a season — this counter is what actually bounds the path.
func getMaxInterimRewardsPerSeason() math.Int {
	return math.NewInt(50_000_000_000) // 50,000 DREAM
}
