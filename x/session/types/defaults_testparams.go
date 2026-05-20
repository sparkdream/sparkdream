//go:build !mainnet && !testnet && !devnet

package types

// Testing-build overrides for recurring-pull defaults. Mirrors the
// pattern used by x/commons/keeper/genesis_vals_testparams.go: when
// no production build tag (mainnet / testnet / devnet) is set, the
// defaults drop to integration-test-friendly values so e2e shell
// scripts can claim within a single test run instead of needing a
// 1-day cadence.
//
// Production defaults stay at 86_400 (1 day cadence floor) and
// 31_536_000 (1 year duration ceiling) — this file is excluded by
// build tag on those tracks.
//
// Triggered by the M11 phase of the RecurringSpend migration: with
// x/commons's `min_recurring_period_seconds` param removed (M10), the
// pre-migration e2e tests' gov-proposal-to-lower-the-floor step no
// longer applies. The session-side floor is the authoritative gate
// post-migration; lowering it here at build time replaces the
// per-test gov dance.
func init() {
	// Lower ONLY the cadence floor so e2e scripts can submit a fast
	// claim loop. The duration ceiling stays at the production value
	// (1 year) — there's no test that needs a tighter ceiling, and
	// lowering it would break existing unit-test fixtures that use
	// 30-day grant expirations.
	DefaultMinRecurringPeriodSeconds = 5 // 5 seconds for tests
}
