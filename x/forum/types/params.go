package types

import "cosmossdk.io/math"

// DefaultMinSentinelBond is the minimum DREAM required to be a sentinel
var DefaultMinSentinelBond = math.NewInt(500)

// Default parameter values
const (
	// Content limits
	DefaultMaxContentSize   = uint64(10240)  // 10KB
	DefaultMaxTagsPerPost   = uint64(5)
	DefaultMaxReplyDepth    = uint32(10)
	DefaultMaxTagLength     = uint64(32)
	DefaultMaxTotalTags     = uint64(10000)

	// Rate limits
	DefaultDailyPostLimit      = uint64(50)
	DefaultMaxReactionsPerDay  = uint64(100)
	DefaultMaxDownvotesPerDay  = uint64(20)
	DefaultMaxFlagsPerDay      = uint64(20)
	DefaultMaxFollowsPerDay    = uint64(50)
	DefaultMaxSalvationsPerDay = uint64(10)

	// Time durations (in seconds)
	DefaultEphemeralTTL            = int64(86400)   // 24h
	DefaultArchiveThreshold        = int64(2592000) // 30 days
	DefaultTagExpiration           = int64(2592000) // 30 days
	DefaultHiddenExpiration        = int64(604800)  // 7 days
	DefaultAppealDeadline          = int64(1209600) // 14 days
	DefaultEditGracePeriod         = int64(300)     // 5 minutes
	DefaultEditMaxWindow           = int64(86400)   // 24 hours
	DefaultArchiveCooldown         = int64(2592000) // 30 days
	DefaultUnarchiveCooldown       = int64(86400)   // 1 day
	DefaultHideAppealCooldown      = int64(3600)    // 1 hour
	DefaultLockAppealCooldown      = int64(3600)    // 1 hour
	DefaultLockAppealDeadline      = int64(1209600) // 14 days
	DefaultMoveAppealCooldown      = int64(3600)    // 1 hour
	DefaultMoveAppealDeadline      = int64(1209600) // 14 days
	DefaultMinMembershipForSalvation = int64(604800) // 7 days
	DefaultBountyDuration          = int64(1209600) // 14 days
	DefaultMaxBountyDuration       = int64(2592000) // 30 days
	DefaultAcceptProposalTimeout   = int64(172800)  // 48 hours
	DefaultMinReportDuration       = int64(172800)  // 48 hours
	DefaultMinDefenseWait          = int64(86400)   // 24 hours
	DefaultMemberReportExpiration  = int64(2592000) // 30 days
	DefaultFlagExpiration          = int64(604800)  // 7 days

	// Sentinel requirements
	DefaultMinRepTierSentinel    = uint64(3) // Tier 3
	DefaultMinRepTierTags        = uint64(2) // Tier 2
	DefaultMinRepTierThreadLock  = uint64(4) // Tier 4

	// Sentinel limits
	DefaultMaxHidesPerEpoch        = uint64(50)
	DefaultMaxSentinelLocksPerEpoch = uint64(5)
	DefaultMaxSentinelMovesPerEpoch = uint64(10)
	DefaultSentinelOverturnCooldown = int64(86400) // 24 hours
	DefaultSentinelDemotionCooldown = int64(604800) // 7 days
	DefaultMinSentinelBondAmount    = int64(500)    // 500 DREAM

	// Archive limits
	DefaultMaxArchivePostCount  = uint64(500)
	DefaultMaxArchiveSizeBytes  = uint64(1048576) // 1MB
	DefaultMaxArchiveCycles     = uint64(5)
	DefaultMaxSalvationDepth    = uint64(10)

	// Pin limits
	DefaultMaxPinnedPerCategory    = uint64(5)
	DefaultMaxPinnedRepliesPerThread = uint64(3)

	// Bounty limits
	DefaultMaxBountyWinners = uint64(5)

	// Flag settings
	DefaultFlagReviewThreshold = uint64(5)
	DefaultMemberFlagWeight    = uint64(2)
	DefaultNonmemberFlagWeight = uint64(1)
	DefaultMaxPostFlaggers     = uint64(50)

	// Report limits
	DefaultMinEvidencePosts           = uint64(3)
	DefaultMemberReportCosignThreshold = uint64(3)
	DefaultMaxWarningsBeforeDemotion  = uint64(3)
	DefaultMaxTagReporters            = uint64(50)
	DefaultMaxMemberReporters         = uint64(20)

	// Appeal default
	DefaultAppealDefaultOutcome = uint32(0) // 0 = restore post

	// Lazy prune
	DefaultLazyPruneLimit = uint64(2)
)

// NewParams creates a new Params instance.
func NewParams() Params {
	return Params{}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams()
}

// Validate validates the set of params.
func (p Params) Validate() error {
	return nil
}

// DefaultMaxContentSizeValue returns the default max content size.
func DefaultMaxContentSizeValue() uint64 {
	return DefaultMaxContentSize
}

// DefaultDailyPostLimitValue returns the default daily post limit.
func DefaultDailyPostLimitValue() uint64 {
	return DefaultDailyPostLimit
}

// DefaultMaxReplyDepthValue returns the default max reply depth.
func DefaultMaxReplyDepthValue() uint32 {
	return DefaultMaxReplyDepth
}

// DefaultEphemeralTTLValue returns the default ephemeral TTL.
func DefaultEphemeralTTLValue() int64 {
	return DefaultEphemeralTTL
}

// DefaultEditGracePeriodValue returns the default edit grace period.
func DefaultEditGracePeriodValue() int64 {
	return DefaultEditGracePeriod
}

// DefaultEditMaxWindowValue returns the default edit max window.
func DefaultEditMaxWindowValue() int64 {
	return DefaultEditMaxWindow
}
