package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultMinSentinelBond is the minimum DREAM required to be a sentinel
var DefaultMinSentinelBond = math.NewInt(500)

// Default fee coin denomination
const DefaultFeeDenom = "uspark"

// Default parameter values
const (
	// Content limits
	DefaultMaxContentSize = uint64(10240) // 10KB
	DefaultMaxTagsPerPost = uint64(5)
	DefaultMaxReplyDepth  = uint32(10)
	DefaultMaxTagLength   = uint64(32)
	DefaultMaxTotalTags   = uint64(10000)

	// Rate limits
	DefaultDailyPostLimit      = uint64(50)
	DefaultMaxReactionsPerDay  = uint64(100)
	DefaultMaxDownvotesPerDay  = uint64(20)
	DefaultMaxFlagsPerDay      = uint64(20)
	DefaultMaxFollowsPerDay    = uint64(50)
	DefaultMaxSalvationsPerDay = uint64(10)

	// Time durations (in seconds)
	DefaultEphemeralTTL              = int64(86400)   // 24h
	DefaultArchiveThreshold          = int64(2592000) // 30 days
	DefaultTagExpiration             = int64(2592000) // 30 days
	DefaultHiddenExpiration          = int64(604800)  // 7 days
	DefaultAppealDeadline            = int64(1209600) // 14 days
	DefaultEditGracePeriod           = int64(300)     // 5 minutes
	DefaultEditMaxWindow             = int64(86400)   // 24 hours
	DefaultArchiveCooldown           = int64(2592000) // 30 days
	DefaultUnarchiveCooldown         = int64(86400)   // 1 day
	DefaultHideAppealCooldown        = int64(3600)    // 1 hour
	DefaultLockAppealCooldown        = int64(3600)    // 1 hour
	DefaultLockAppealDeadline        = int64(1209600) // 14 days
	DefaultMoveAppealCooldown        = int64(3600)    // 1 hour
	DefaultMoveAppealDeadline        = int64(1209600) // 14 days
	DefaultMinMembershipForSalvation = int64(604800)  // 7 days
	DefaultBountyDuration            = int64(1209600) // 14 days
	DefaultMaxBountyDuration         = int64(2592000) // 30 days
	DefaultAcceptProposalTimeout     = int64(172800)  // 48 hours
	DefaultMinReportDuration         = int64(172800)  // 48 hours
	DefaultMinDefenseWait            = int64(86400)   // 24 hours
	DefaultMemberReportExpiration    = int64(2592000) // 30 days
	DefaultFlagExpiration            = int64(604800)  // 7 days

	// Sentinel requirements
	DefaultMinRepTierSentinel   = uint64(3) // Tier 3
	DefaultMinRepTierTags       = uint64(2) // Tier 2
	DefaultMinRepTierThreadLock = uint64(4) // Tier 4

	// Sentinel limits
	DefaultMaxHidesPerEpoch         = uint64(50)
	DefaultMaxSentinelLocksPerEpoch = uint64(5)
	DefaultMaxSentinelMovesPerEpoch = uint64(10)
	DefaultSentinelOverturnCooldown = int64(86400)  // 24 hours
	DefaultSentinelDemotionCooldown = int64(604800) // 7 days
	DefaultMinSentinelBondAmount    = int64(500)    // 500 DREAM
	DefaultSentinelSlashAmount      = int64(100)    // 100 DREAM per overturned appeal

	// Archive limits
	DefaultMaxArchiveCycles  = uint64(5)
	DefaultMaxSalvationDepth = uint64(10)

	// Pin limits
	DefaultMaxPinnedPerCategory      = uint64(5)
	DefaultMaxPinnedRepliesPerThread = uint64(3)

	// Bounty limits
	DefaultMaxBountyWinners             = uint64(5)
	DefaultBountyCancellationFeePercent = uint64(10) // 10%

	// Flag settings
	DefaultFlagReviewThreshold = uint64(5)
	DefaultMemberFlagWeight    = uint64(2)
	DefaultNonmemberFlagWeight = uint64(1)
	DefaultMaxPostFlaggers     = uint64(50)

	// Report limits
	DefaultMinEvidencePosts            = uint64(3)
	DefaultMemberReportCosignThreshold = uint64(3)
	DefaultMaxWarningsBeforeDemotion   = uint64(3)
	DefaultMaxTagReporters             = uint64(50)
	DefaultMaxMemberReporters          = uint64(20)

	// Appeal default
	DefaultAppealDefaultOutcome = uint32(0) // 0 = restore post

	// Lazy prune
	DefaultLazyPruneLimit = uint64(2)

	// Anonymous posting defaults
	DefaultAnonymousPostingEnabled = true
	DefaultAnonymousMinTrustLevel  = uint32(2) // ESTABLISHED
	DefaultPrivateReactionsEnabled = true

	// Conviction renewal defaults
	DefaultConvictionRenewalPeriod = int64(604800) // 7 days
)

// Default fee amounts
var (
	DefaultSpamTaxAmount         = math.NewInt(1000000) // 1 SPARK
	DefaultReactionSpamTaxAmount = math.NewInt(100000)  // 0.1 SPARK
	DefaultFlagSpamTaxAmount     = math.NewInt(100000)  // 0.1 SPARK
	DefaultDownvoteDepositAmount = math.NewInt(50000)   // 0.05 SPARK
	DefaultAppealFeeAmount       = math.NewInt(5000000) // 5 SPARK
	DefaultLockAppealFeeAmount   = math.NewInt(5000000) // 5 SPARK
	DefaultMoveAppealFeeAmount   = math.NewInt(5000000) // 5 SPARK
	DefaultEditFeeAmount         = math.NewInt(10000)   // 0.01 SPARK
	DefaultTagReportBond         = math.NewInt(10)      // 10 DREAM
	DefaultCostPerByteAmount             = math.NewInt(100)     // 100 uspark/byte (~1 SPARK for 10KB)
	DefaultConvictionRenewalThreshold    = math.LegacyNewDec(100)
)

// NewParams creates a new Params instance.
func NewParams() Params {
	return Params{
		ForumPaused:                  false,
		ModerationPaused:             false,
		BountiesEnabled:              true,
		ReactionsEnabled:             true,
		AppealsPaused:                false,
		EditingEnabled:               true,
		SpamTax:                      sdk.NewCoin(DefaultFeeDenom, DefaultSpamTaxAmount),
		ReactionSpamTax:              sdk.NewCoin(DefaultFeeDenom, DefaultReactionSpamTaxAmount),
		FlagSpamTax:                  sdk.NewCoin(DefaultFeeDenom, DefaultFlagSpamTaxAmount),
		DownvoteDeposit:              sdk.NewCoin(DefaultFeeDenom, DefaultDownvoteDepositAmount),
		AppealFee:                    sdk.NewCoin(DefaultFeeDenom, DefaultAppealFeeAmount),
		LockAppealFee:                sdk.NewCoin(DefaultFeeDenom, DefaultLockAppealFeeAmount),
		MoveAppealFee:                sdk.NewCoin(DefaultFeeDenom, DefaultMoveAppealFeeAmount),
		EditFee:                      sdk.NewCoin(DefaultFeeDenom, DefaultEditFeeAmount),
		BountyCancellationFeePercent: DefaultBountyCancellationFeePercent,
		MaxContentSize:               DefaultMaxContentSize,
		DailyPostLimit:               DefaultDailyPostLimit,
		MaxReplyDepth:                DefaultMaxReplyDepth,
		EditGracePeriod:              DefaultEditGracePeriod,
		EditMaxWindow:                DefaultEditMaxWindow,
		MaxFollowsPerDay:             DefaultMaxFollowsPerDay,
		ArchiveThreshold:             DefaultArchiveThreshold,
		UnarchiveCooldown:            DefaultUnarchiveCooldown,
		ArchiveCooldown:              DefaultArchiveCooldown,
		HideAppealCooldown:           DefaultHideAppealCooldown,
		LockAppealCooldown:           DefaultLockAppealCooldown,
		MoveAppealCooldown:           DefaultMoveAppealCooldown,
		CostPerByte:                  sdk.NewCoin(DefaultFeeDenom, DefaultCostPerByteAmount),
		CostPerByteExempt:            false,
		EphemeralTtl:                 DefaultEphemeralTTL,
		AnonymousPostingEnabled:      DefaultAnonymousPostingEnabled,
		AnonymousMinTrustLevel:       DefaultAnonymousMinTrustLevel,
		PrivateReactionsEnabled:      DefaultPrivateReactionsEnabled,
		AnonSubsidyBudgetPerEpoch:    sdk.NewCoin(DefaultFeeDenom, math.ZeroInt()),
		AnonSubsidyMaxPerPost:        sdk.NewCoin(DefaultFeeDenom, math.ZeroInt()),
		AnonSubsidyApprovedRelays:    nil,
		ConvictionRenewalThreshold:   DefaultConvictionRenewalThreshold,
		ConvictionRenewalPeriod:      DefaultConvictionRenewalPeriod,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams()
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if !p.CostPerByte.Amount.IsNil() && p.CostPerByte.IsNegative() {
		return fmt.Errorf("cost_per_byte cannot be negative: %s", p.CostPerByte)
	}
	if p.EphemeralTtl <= 0 {
		return fmt.Errorf("ephemeral_ttl must be positive: %d", p.EphemeralTtl)
	}
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

// DefaultForumOperationalParams returns default operational parameters.
func DefaultForumOperationalParams() ForumOperationalParams {
	return ForumOperationalParams{
		BountiesEnabled:              true,
		ReactionsEnabled:             true,
		EditingEnabled:               true,
		SpamTax:                      sdk.NewCoin(DefaultFeeDenom, DefaultSpamTaxAmount),
		ReactionSpamTax:              sdk.NewCoin(DefaultFeeDenom, DefaultReactionSpamTaxAmount),
		FlagSpamTax:                  sdk.NewCoin(DefaultFeeDenom, DefaultFlagSpamTaxAmount),
		DownvoteDeposit:              sdk.NewCoin(DefaultFeeDenom, DefaultDownvoteDepositAmount),
		AppealFee:                    sdk.NewCoin(DefaultFeeDenom, DefaultAppealFeeAmount),
		LockAppealFee:                sdk.NewCoin(DefaultFeeDenom, DefaultLockAppealFeeAmount),
		MoveAppealFee:                sdk.NewCoin(DefaultFeeDenom, DefaultMoveAppealFeeAmount),
		EditFee:                      sdk.NewCoin(DefaultFeeDenom, DefaultEditFeeAmount),
		CostPerByte:                  sdk.NewCoin(DefaultFeeDenom, DefaultCostPerByteAmount),
		CostPerByteExempt:            false,
		MaxContentSize:               DefaultMaxContentSize,
		DailyPostLimit:               DefaultDailyPostLimit,
		MaxReplyDepth:                DefaultMaxReplyDepth,
		MaxFollowsPerDay:             DefaultMaxFollowsPerDay,
		BountyCancellationFeePercent: DefaultBountyCancellationFeePercent,
		EditGracePeriod:              DefaultEditGracePeriod,
		EditMaxWindow:                DefaultEditMaxWindow,
		ArchiveThreshold:             DefaultArchiveThreshold,
		UnarchiveCooldown:            DefaultUnarchiveCooldown,
		ArchiveCooldown:              DefaultArchiveCooldown,
		HideAppealCooldown:           DefaultHideAppealCooldown,
		LockAppealCooldown:           DefaultLockAppealCooldown,
		MoveAppealCooldown:           DefaultMoveAppealCooldown,
		EphemeralTtl:                 DefaultEphemeralTTL,
		AnonymousPostingEnabled:      DefaultAnonymousPostingEnabled,
		AnonymousMinTrustLevel:       DefaultAnonymousMinTrustLevel,
		PrivateReactionsEnabled:      DefaultPrivateReactionsEnabled,
		ConvictionRenewalThreshold:   DefaultConvictionRenewalThreshold,
		ConvictionRenewalPeriod:      DefaultConvictionRenewalPeriod,
	}
}

// Validate validates the operational parameters.
func (p ForumOperationalParams) Validate() error {
	if p.EphemeralTtl <= 0 {
		return fmt.Errorf("ephemeral_ttl must be positive: %d", p.EphemeralTtl)
	}
	if !p.CostPerByte.Amount.IsNil() && p.CostPerByte.IsNegative() {
		return fmt.Errorf("cost_per_byte cannot be negative: %s", p.CostPerByte)
	}
	if p.BountyCancellationFeePercent > 100 {
		return fmt.Errorf("bounty_cancellation_fee_percent must be <= 100: %d", p.BountyCancellationFeePercent)
	}
	if p.ConvictionRenewalThreshold.IsNegative() {
		return fmt.Errorf("conviction_renewal_threshold cannot be negative: %s", p.ConvictionRenewalThreshold)
	}
	if p.ConvictionRenewalPeriod < 0 {
		return fmt.Errorf("conviction_renewal_period cannot be negative: %d", p.ConvictionRenewalPeriod)
	}
	return nil
}

// ApplyOperationalParams copies all operational fields from ForumOperationalParams
// onto the full Params, preserving non-operational fields (forum_paused, moderation_paused, appeals_paused).
func (p Params) ApplyOperationalParams(op ForumOperationalParams) Params {
	p.BountiesEnabled = op.BountiesEnabled
	p.ReactionsEnabled = op.ReactionsEnabled
	p.EditingEnabled = op.EditingEnabled
	p.SpamTax = op.SpamTax
	p.ReactionSpamTax = op.ReactionSpamTax
	p.FlagSpamTax = op.FlagSpamTax
	p.DownvoteDeposit = op.DownvoteDeposit
	p.AppealFee = op.AppealFee
	p.LockAppealFee = op.LockAppealFee
	p.MoveAppealFee = op.MoveAppealFee
	p.EditFee = op.EditFee
	p.CostPerByte = op.CostPerByte
	p.CostPerByteExempt = op.CostPerByteExempt
	p.MaxContentSize = op.MaxContentSize
	p.DailyPostLimit = op.DailyPostLimit
	p.MaxReplyDepth = op.MaxReplyDepth
	p.MaxFollowsPerDay = op.MaxFollowsPerDay
	p.BountyCancellationFeePercent = op.BountyCancellationFeePercent
	p.EditGracePeriod = op.EditGracePeriod
	p.EditMaxWindow = op.EditMaxWindow
	p.ArchiveThreshold = op.ArchiveThreshold
	p.UnarchiveCooldown = op.UnarchiveCooldown
	p.ArchiveCooldown = op.ArchiveCooldown
	p.HideAppealCooldown = op.HideAppealCooldown
	p.LockAppealCooldown = op.LockAppealCooldown
	p.MoveAppealCooldown = op.MoveAppealCooldown
	p.EphemeralTtl = op.EphemeralTtl
	p.AnonymousPostingEnabled = op.AnonymousPostingEnabled
	p.AnonymousMinTrustLevel = op.AnonymousMinTrustLevel
	p.PrivateReactionsEnabled = op.PrivateReactionsEnabled
	p.ConvictionRenewalThreshold = op.ConvictionRenewalThreshold
	p.ConvictionRenewalPeriod = op.ConvictionRenewalPeriod
	return p
}

// ExtractOperationalParams extracts the operational fields from Params into ForumOperationalParams.
func (p Params) ExtractOperationalParams() ForumOperationalParams {
	return ForumOperationalParams{
		BountiesEnabled:              p.BountiesEnabled,
		ReactionsEnabled:             p.ReactionsEnabled,
		EditingEnabled:               p.EditingEnabled,
		SpamTax:                      p.SpamTax,
		ReactionSpamTax:              p.ReactionSpamTax,
		FlagSpamTax:                  p.FlagSpamTax,
		DownvoteDeposit:              p.DownvoteDeposit,
		AppealFee:                    p.AppealFee,
		LockAppealFee:                p.LockAppealFee,
		MoveAppealFee:                p.MoveAppealFee,
		EditFee:                      p.EditFee,
		CostPerByte:                  p.CostPerByte,
		CostPerByteExempt:            p.CostPerByteExempt,
		MaxContentSize:               p.MaxContentSize,
		DailyPostLimit:               p.DailyPostLimit,
		MaxReplyDepth:                p.MaxReplyDepth,
		MaxFollowsPerDay:             p.MaxFollowsPerDay,
		BountyCancellationFeePercent: p.BountyCancellationFeePercent,
		EditGracePeriod:              p.EditGracePeriod,
		EditMaxWindow:                p.EditMaxWindow,
		ArchiveThreshold:             p.ArchiveThreshold,
		UnarchiveCooldown:            p.UnarchiveCooldown,
		ArchiveCooldown:              p.ArchiveCooldown,
		HideAppealCooldown:           p.HideAppealCooldown,
		LockAppealCooldown:           p.LockAppealCooldown,
		MoveAppealCooldown:           p.MoveAppealCooldown,
		EphemeralTtl:                 p.EphemeralTtl,
		AnonymousPostingEnabled:      p.AnonymousPostingEnabled,
		AnonymousMinTrustLevel:       p.AnonymousMinTrustLevel,
		PrivateReactionsEnabled:      p.PrivateReactionsEnabled,
		ConvictionRenewalThreshold:   p.ConvictionRenewalThreshold,
		ConvictionRenewalPeriod:      p.ConvictionRenewalPeriod,
	}
}
