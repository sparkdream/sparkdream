//go:build mainnet || testnet || devnet

package types

// Production values (docs/x-artifact-spec.md §8, §3.13). 14400 blocks ≈ 1 day.

const (
	// Seconds.
	MinPendingTTL         int64 = 24 * 3600
	MaxPendingTTL         int64 = 30 * 24 * 3600
	MinMaxListingDuration int64 = 24 * 3600
	MaxMaxListingDuration int64 = 365 * 24 * 3600

	// Block heights.
	MinModerationBlocks int64 = 14400
	MaxModerationBlocks int64 = 90 * 14400
)

func defaultPendingTTL() int64         { return 7 * 24 * 3600 }
func defaultMaxListingDuration() int64 { return 90 * 24 * 3600 }
func defaultUnhideWindowBlocks() int64 { return 14400 }
func defaultHideExpiryBlocks() int64   { return 7 * 14400 }
