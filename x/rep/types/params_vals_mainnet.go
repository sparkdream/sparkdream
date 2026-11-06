//go:build mainnet

package types

import (
	"time"

	"cosmossdk.io/math"
)

// Mainnet values for trust level configuration.
// Build with: go build -tags mainnet
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(50),
		ProvisionalMinInterims:       3,
		EstablishedMinRep:            math.LegacyNewDec(200),
		EstablishedMinInterims:       10,
		TrustedMinRep:                math.LegacyNewDec(500),
		TrustedMinSeasons:            1,
		CoreMinRep:                   math.LegacyNewDec(1000),
		CoreMinSeasons:               2,
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 3,
		EstablishedInvitationCredits: 6,
		TrustedInvitationCredits:     10,
		CoreInvitationCredits:        20,
	}
}

// getSentinelRewardEpochBlocks returns the cadence at which the sentinel SPARK
// reward pool is drained on mainnet (~1 day at 6s blocks).
func getSentinelRewardEpochBlocks() uint64 {
	return 14400
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
	return 100800 // 7d challenge_window
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

// getInvitationCostMultiplier returns the per-invitation cost escalation
// applied on top of MinInvitationStake. 1.1x raised to the credits-spent
// power makes spam invitations exponentially more expensive while leaving
// the first invitation at the floor.
func getInvitationCostMultiplier() math.LegacyDec {
	return math.LegacyNewDecWithPrec(110, 2) // 1.1x
}

// getMaxInterimRewardsPerSeason is the per-season ceiling on DREAM minted to
// pay interim work. Members propose interims for themselves and the Operations
// Committee approves the payment; max_active_interims_per_member bounds only
// how many are open at once, not how many are paid in a season — this counter
// is what bounds the path in total.
func getMaxInterimRewardsPerSeason() math.Int {
	return math.NewInt(50_000_000_000) // 50,000 DREAM
}

// getMigrationTimings returns the member key migration windows (production
// defaults, see productionMigrationTimings).
func getMigrationTimings() migrationTimings { return productionMigrationTimings() }

// getAliasDedupHorizonEpochs is the production dedup horizon of the member
// key migration: 240 epochs, which covers a 150-epoch season with its 7-epoch
// transition and 14-epoch extension cap.
func getAliasDedupHorizonEpochs() int64 { return 240 }

// getMaxAnonymousActionWindow is this network's ceiling on any window in
// which one anonymous member may act once per item against the live member
// tree (today the federation arbiter vote). The ZK key activation delay must
// cover it, see Params.Validate. 7 days: the longest arbiter window the spec ranges allow, half the 14-day delay.
func getMaxAnonymousActionWindow() time.Duration { return 7 * 24 * time.Hour }

// getJuryBeaconWaitEpochs is how many shield epochs a queued jury draw waits
// for its beacon value before falling back to the block-hash seed. The seed
// epoch is the one after the epoch current at queueing, so its key arrives
// up to two epoch intervals plus a block or two later. With batch mode on,
// an epoch holding a batch under min_batch_size is not demanded until
// max_pending_epochs (6) have passed, so seven intervals keep an honest
// beacon from ever forcing a fallback.
func getJuryBeaconWaitEpochs() uint64 { return 7 }
