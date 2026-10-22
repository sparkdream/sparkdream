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

	// Expiring tokens: class term bounds (seconds).
	MinTokenTerm int64 = 3600
	MaxTokenTerm int64 = 100 * 365 * 24 * 3600
	// Commit-reveal: a reveal deadline must be at least this far after
	// class creation (seconds).
	MinRevealWindow int64 = 24 * 3600
	// Harberger tax period and foreclosure bounds (seconds).
	MinHarbergerPeriod int64 = 24 * 3600
	MaxHarbergerPeriod int64 = 365 * 24 * 3600
	// Rental duration bounds for max_rental_duration (seconds).
	MinRentalDuration    int64 = 3600
	MaxRentalDurationCap int64 = 365 * 24 * 3600
	// Anti-sniping extension bounds (seconds).
	MinAuctionExtension int64 = 60
	MaxAuctionExtension int64 = 24 * 3600
)

func defaultPendingTTL() int64         { return 7 * 24 * 3600 }
func defaultMaxListingDuration() int64 { return 90 * 24 * 3600 }
func defaultUnhideWindowBlocks() int64 { return 14400 }
func defaultHideExpiryBlocks() int64   { return 7 * 14400 }
func defaultAuctionExtension() int64   { return 600 }
