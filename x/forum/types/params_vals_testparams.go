//go:build !mainnet && !testnet && !devnet

package types

// hiddenExpirationDefault returns the time (in seconds) a HIDDEN post
// lingers before ExpireHiddenPosts soft-deletes it.
//
// Testparams shortens this from production's 7 days to 15 seconds so the
// unappealed-hide finalization (and its author rep slash + promoter warning
// hooks) can be exercised end-to-end inside a single shell test run.
// 15s ≈ 2-3 blocks at the testparams block time, enough that the
// EndBlocker pass observably fires.
func hiddenExpirationDefault() int64 { return 15 }

// opsClockFloorsEnabled is off in the test build: e2e genesis pins second-long
// windows, and committee updates in e2e re-submit them. Ceilings still apply.
const opsClockFloorsEnabled = false

// hideAppealCooldownDefault: 5 seconds, inside the 15-second hidden expiration (config.yml pins the same).
func hideAppealCooldownDefault() int64 { return 5 }
