//go:build !mainnet && !testnet && !devnet

package types

// Testparams values: short windows so e2e scripts can exercise inbox
// expiry, listing expiry and the hide lifecycle inside one run. The hard
// bounds scale down with them.

const (
	// Seconds.
	MinPendingTTL         int64 = 5
	MaxPendingTTL         int64 = 30 * 24 * 3600
	MinMaxListingDuration int64 = 5
	MaxMaxListingDuration int64 = 365 * 24 * 3600

	// Block heights.
	MinModerationBlocks int64 = 1
	MaxModerationBlocks int64 = 90 * 14400

	// Expiring tokens: class term bounds (seconds).
	MinTokenTerm int64 = 5
	MaxTokenTerm int64 = 100 * 365 * 24 * 3600
	// Commit-reveal: a reveal deadline must be at least this far after
	// class creation (seconds).
	MinRevealWindow int64 = 5
)

func defaultPendingTTL() int64         { return 30 }
func defaultMaxListingDuration() int64 { return 90 * 24 * 3600 }
func defaultUnhideWindowBlocks() int64 { return 5 }
func defaultHideExpiryBlocks() int64   { return 10 }
