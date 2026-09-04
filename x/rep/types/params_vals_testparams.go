//go:build !mainnet && !testnet && !devnet

package types

import (
	"cosmossdk.io/math"
)

// Testing values — reduced for faster trust level progression during integration tests.
// This is the default when no build tag is specified, or with: go build -tags testparams
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(10),  // production: 50
		ProvisionalMinInterims:       1,                      // production: 3
		EstablishedMinRep:            math.LegacyNewDec(50),  // production: 200
		EstablishedMinInterims:       3,                      // production: 10
		TrustedMinRep:                math.LegacyNewDec(100), // production: 500
		TrustedMinSeasons:            0,                      // production: 1
		CoreMinRep:                   math.LegacyNewDec(200), // production: 1000
		CoreMinSeasons:               0,                      // production: 2
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 2,  // production: 1
		EstablishedInvitationCredits: 5,  // production: 3
		TrustedInvitationCredits:     10, // production: 5
		CoreInvitationCredits:        20, // production: 10
	}
}

// getSentinelRewardEpochBlocks returns the cadence at which the sentinel SPARK
// reward pool is drained. Testparams uses a very short epoch (20 blocks) so E2E
// tests can exercise it quickly.
func getSentinelRewardEpochBlocks() uint64 {
	return 20
}

// getVerifierRewardEpochBlocks returns the federation-verifier reward
// cadence. Deliberately its own dial rather than the sentinel's: a
// verification is challengeable for far longer than a hide is appealable, so
// the epoch has to be long enough that a challenge filed against work in it
// can plausibly resolve before the accuracy window scores that work.
//
// Testparams is the ONE network that breaks that rule on purpose. The other
// three set this to a full challenge_window in blocks; here that would be 50
// (5m at 6s), and the RECOVERY auto-bond test has to wait through two whole
// epochs -- slash in epoch N, auto-bond in epoch N+1 -- which at 50 would be
// ~10 minutes of wall time in a suite that already runs long. 10 blocks (~60s)
// keeps that under ~2 minutes. The cost is that accuracy can be scored before
// a challenge resolves, which is exactly why min_verifier_accuracy is relaxed
// to 0 here (see getMinVerifierAccuracy) rather than left to fire on a stale
// verdict count.
func getVerifierRewardEpochBlocks() uint64 {
	return 10
}

// Verifier-pay eligibility, relaxed for E2E. These moved here from
// x/federation's genesis_vals_testparams.go when verifier pay migrated to
// x/rep; test/federation/verifier_rewards_test.sh depends on all three.
//
// getMinEpochVerifications is the per-epoch verification floor. 1 (prod: 3)
// so a verifier clears it with a single submission inside a 10-block epoch.
func getMinEpochVerifications() uint32 {
	return 1
}

// getMinVerifierAccuracy is the windowed-accuracy bar. 0% (prod: 0.80) so a
// deliberately-overturned verifier can still earn on fresh activity, which is
// what the RECOVERY auto-bond test needs to observe.
func getMinVerifierAccuracy() math.LegacyDec {
	return math.LegacyZeroDec()
}

// getMaxVerifierDreamMintPerEpoch caps the DREAM stipend across all eligible
// verifiers. 7 DREAM (prod: 100) against a 5 DREAM per-verifier reward, so two
// eligible verifiers force the pro-rata scaling path the cap-scaling test
// asserts on.
func getMaxVerifierDreamMintPerEpoch() math.Int {
	return math.NewInt(7000000)
}

// getInvitationCostMultiplier disables per-invitation cost escalation in
// testparams so a single founder (alice) can fan out 13+ invitations during
// E2E setup without exhausting their genesis DREAM balance. Production keeps
// the 1.1x escalation as an anti-sybil deterrent.
func getInvitationCostMultiplier() math.LegacyDec {
	return math.LegacyOneDec()
}

// getMaxInterimRewardsPerSeason is the per-season interim emission ceiling.
// Testparams raises it far above the production 50,000 DREAM because the E2E
// setup alone bootstraps reputation with 24 EPIC interims (1,000 DREAM each
// plus the 50% solo-expert bonus = ~36,000 DREAM) before a single test runs,
// and several suites create more. The cap's behaviour is covered directly by
// TestInterim_SeasonCapBoundsTotalEmission rather than by starving the suites.
func getMaxInterimRewardsPerSeason() math.Int {
	return math.NewInt(100_000_000_000_000) // 100,000,000 DREAM — effectively unbounded for tests
}
