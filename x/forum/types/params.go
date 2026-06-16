package types

import (
	"fmt"

	"cosmossdk.io/math"

	reptypes "sparkdream/x/rep/types"
)

// DefaultMinSentinelBond is the minimum DREAM required to be a sentinel (in udream).
var DefaultMinSentinelBond = math.NewInt(500_000_000) // 500 DREAM

// DefaultLockBackingAmount is the default minimum DREAM backing (total balance)
// a sentinel must hold to lock a thread (in udream). Governance-tunable but
// bounded >= the derived lock bond in Params.Validate().
var DefaultLockBackingAmount = math.NewInt(20_000_000_000) // 20000 DREAM

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
	DefaultAppealDeadline            = int64(1209600) // 14 days
	DefaultEditGracePeriod           = int64(300)     // 5 minutes
	DefaultEditMaxWindow             = int64(86400)   // 24 hours
	DefaultSentinelUnhideWindow      = int64(86400)   // 24 hours; sentinel self-correct window for MsgUnhidePost
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
	DefaultFlagExpiration            = int64(604800)  // 7 days

	// Sentinel requirements
	DefaultMinRepTierSentinel   = uint64(0) // No rep-tier floor; trust-level gate + bond + accuracy metrics are the eligibility filters
	DefaultMinRepTierTags       = uint64(2) // Tier 2
	DefaultMinRepTierThreadLock = uint64(4) // Tier 4

	// Sentinel limits
	DefaultMaxHidesPerEpoch         = uint64(50)
	DefaultMaxSentinelLocksPerEpoch = uint64(5)
	DefaultMaxSentinelMovesPerEpoch = uint64(10)

	// ModerationEpochCapCeiling is the upper bound for the per-action epoch
	// caps (hides/locks/moves). A sanity ceiling so a stored cap can't be set
	// absurdly high; well above any realistic per-day moderation volume.
	ModerationEpochCapCeiling = uint64(10_000)

	// DefaultLockBondMultiplier: the lock bond floor is
	// lock_bond_multiplier × min_sentinel_bond. Default 4 → 4 × 500 = 2000
	// DREAM, matching the historical hardcoded lock bond. Bounded >= 1 in
	// Params.Validate() so locking is never weaker than the base bond.
	DefaultLockBondMultiplier       = uint64(4)
	DefaultSentinelOverturnCooldown = int64(86400)       // 24 hours
	DefaultSentinelDemotionCooldown = int64(604800)      // 7 days
	DefaultMinSentinelBondAmount    = int64(500_000_000) // 500 DREAM (in udream)
	DefaultSentinelSlashAmount      = int64(100_000_000) // 100 DREAM per overturned appeal (in udream)
	// DefaultSentinelUnbondCooldown keeps a sentinel's bond locked and
	// slashable for 14 days after MsgUnbondRole — longer than the 7-day
	// demotion cooldown to ensure overturns flagged during the unbond window
	// can still slash before exit. Mirrors federation's bridge_unbonding_period.
	DefaultSentinelUnbondCooldown = int64(1209600) // 14 days

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

	// Appeal default
	DefaultAppealDefaultOutcome = uint32(0) // 0 = restore post

	// Lazy prune
	DefaultLazyPruneLimit = uint64(2)

	// Conviction renewal defaults
	DefaultConvictionRenewalPeriod = int64(604800) // 7 days

	// DefaultMakePermanentMinTrustLevel is the default minimum trust level for
	// MsgMakePostPermanent (PROVISIONAL = 1). Lower than the sentinel-pin gate
	// because preservation is a smaller curator action than featuring.
	DefaultMakePermanentMinTrustLevel = uint32(1)

	// DefaultMaxPromotionsPerBlock is the per-block cap on the EndBlocker
	// membership-promotion drain. 50 is enough to clear a typical new-member
	// backlog within a few blocks without blowing block gas.
	DefaultMaxPromotionsPerBlock = uint32(50)

	// DefaultMaxMakePermanentPerDay caps MsgMakePostPermanent per address per
	// day. Independent of DailyPostLimit — promotion is a distinct curator
	// action with its own quota.
	DefaultMaxMakePermanentPerDay = uint64(10)
)

// DefaultHiddenExpiration is the time (in seconds) a HIDDEN post lingers
// before ExpireHiddenPosts soft-deletes it. Production is 7 days; the
// testparams build (default in CI / E2E shell tests) shortens it via
// hiddenExpirationDefault() so test runs can observe the EndBlocker
// finalization (and its slash hooks) within a few blocks.
var DefaultHiddenExpiration = hiddenExpirationDefault()

// Default fee amounts (all in bond-denom micro-units).
var (
	DefaultSpamTaxAmount              = math.NewInt(1000000)    // 1 SPARK
	DefaultReactionSpamTaxAmount      = math.NewInt(100000)     // 0.1 SPARK
	DefaultFlagSpamTaxAmount          = math.NewInt(100000)     // 0.1 SPARK
	DefaultDownvoteDepositAmount      = math.NewInt(50000)      // 0.05 SPARK
	DefaultAppealFeeAmount            = math.NewInt(5000000)    // 5 SPARK
	DefaultLockAppealFeeAmount        = math.NewInt(5000000)    // 5 SPARK
	DefaultMoveAppealFeeAmount        = math.NewInt(5000000)    // 5 SPARK
	DefaultEditFeeAmount              = math.NewInt(10000)      // 0.01 SPARK
	DefaultTagReportBond              = math.NewInt(10_000_000) // 10 DREAM (in udream)
	DefaultCostPerByteAmount          = math.NewInt(100)        // 100 micro-units/byte (~1 SPARK for 10KB)
	DefaultConvictionRenewalThreshold = math.LegacyNewDec(100)

	// DefaultAuthorRepSlash is the per-tag reputation deduction applied to a
	// post's author when an unappealed sentinel hide finalizes for the post.
	// 5.0 is a low-but-noticeable amount — enough to bite established
	// contributors without zeroing a marginal one in a single bad post.
	DefaultAuthorRepSlash = math.LegacyNewDec(5)

	// DefaultMinPostConvictionStake is the minimum DREAM (in uDREAM) required
	// to open a PostConvictionStake. 10 DREAM = 10_000_000 uDREAM floors out
	// sybil dust stakes that would otherwise farm the per-tag epoch cap with
	// many tiny accounts.
	DefaultMinPostConvictionStake = math.NewInt(10_000_000)

	// DefaultPostConvictionRepPerDreamPerDay is the per-DREAM-per-day rep
	// accrual rate. 0.05 means a 10-DREAM stake held for 14 days accrues
	// 10 * 14 * 0.05 = 7 forum-rep total, split across the post's tags.
	// Sized so a meaningful endorsement nudges ESTABLISHED admission but
	// no single stake catapults anyone there.
	DefaultPostConvictionRepPerDreamPerDay = math.LegacyMustNewDecFromStr("0.05")

	// DefaultMaxForumRepPerTagPerEpoch caps per-(author, tag) forum-rep
	// accrual per UTC day. 5.0 lets a popular post saturate the cap from a
	// few coordinated stakers without enabling unbounded farming. Excess is
	// silently dropped (no error, no refund) per the spec — honest stakers
	// cannot foresee saturation.
	DefaultMaxForumRepPerTagPerEpoch = math.LegacyNewDec(5)
)

const (
	// DefaultPostConvictionLockSeconds is the default DREAM-lock window for
	// a PostConvictionStake. 14 days mirrors common slash-cooldown windows
	// elsewhere in the protocol and is long enough that the staker has real
	// skin in the game while the hide-appeal window completes.
	DefaultPostConvictionLockSeconds = int64(14 * 86400)

	// DefaultPostConvictionStakerSlashBps is the staker slash applied (in
	// basis points) when SlashStakesForPost runs on a confirmed hide.
	// 25% of locked DREAM is burned — meaningful skin in the game without
	// being so punitive that ESTABLISHED+ members refuse to stake.
	DefaultPostConvictionStakerSlashBps = uint64(2500)
)

// NewParams creates a new Params instance.
func NewParams() Params {
	return Params{
		ForumPaused:                      false,
		ModerationPaused:                 false,
		BountiesEnabled:                  true,
		ReactionsEnabled:                 true,
		AppealsPaused:                    false,
		EditingEnabled:                   true,
		SpamTaxAmount:                    DefaultSpamTaxAmount,
		ReactionSpamTaxAmount:            DefaultReactionSpamTaxAmount,
		FlagSpamTaxAmount:                DefaultFlagSpamTaxAmount,
		DownvoteDepositAmount:            DefaultDownvoteDepositAmount,
		AppealFeeAmount:                  DefaultAppealFeeAmount,
		LockAppealFeeAmount:              DefaultLockAppealFeeAmount,
		MoveAppealFeeAmount:              DefaultMoveAppealFeeAmount,
		EditFeeAmount:                    DefaultEditFeeAmount,
		BountyCancellationFeePercent:     DefaultBountyCancellationFeePercent,
		MaxContentSize:                   DefaultMaxContentSize,
		DailyPostLimit:                   DefaultDailyPostLimit,
		MaxReplyDepth:                    DefaultMaxReplyDepth,
		EditGracePeriod:                  DefaultEditGracePeriod,
		EditMaxWindow:                    DefaultEditMaxWindow,
		MaxFollowsPerDay:                 DefaultMaxFollowsPerDay,
		ArchiveThreshold:                 DefaultArchiveThreshold,
		UnarchiveCooldown:                DefaultUnarchiveCooldown,
		ArchiveCooldown:                  DefaultArchiveCooldown,
		HideAppealCooldown:               DefaultHideAppealCooldown,
		LockAppealCooldown:               DefaultLockAppealCooldown,
		MoveAppealCooldown:               DefaultMoveAppealCooldown,
		CostPerByteAmount:                DefaultCostPerByteAmount,
		CostPerByteExempt:                false,
		EphemeralTtl:                     DefaultEphemeralTTL,
		ConvictionRenewalThreshold:       DefaultConvictionRenewalThreshold,
		ConvictionRenewalPeriod:          DefaultConvictionRenewalPeriod,
		MinSentinelBond:                  DefaultMinSentinelBond.String(),
		MinSentinelRepTier:               DefaultMinRepTierSentinel,
		MinSentinelTrustLevel:            "TRUST_LEVEL_ESTABLISHED",
		MinSentinelAgeBlocks:             0,
		SentinelDemotionCooldown:         DefaultSentinelDemotionCooldown,
		SentinelDemotionThreshold:        math.NewInt(DefaultSentinelDemotionThresholdAmount).String(),
		SentinelUnhideWindow:             DefaultSentinelUnhideWindow,
		SentinelUnbondCooldown:           DefaultSentinelUnbondCooldown,
		MakePermanentMinTrustLevel:       DefaultMakePermanentMinTrustLevel,
		MaxPromotionsPerBlock:            DefaultMaxPromotionsPerBlock,
		AuthorRepSlash:                   DefaultAuthorRepSlash,
		MaxMakePermanentPerDay:           DefaultMaxMakePermanentPerDay,
		MaxHidesPerEpoch:                 DefaultMaxHidesPerEpoch,
		MaxSentinelLocksPerEpoch:         DefaultMaxSentinelLocksPerEpoch,
		MaxSentinelMovesPerEpoch:         DefaultMaxSentinelMovesPerEpoch,
		SentinelSlashAmount:              math.NewInt(DefaultSentinelSlashAmount).String(),
		LockBondMultiplier:               DefaultLockBondMultiplier,
		LockBackingAmount:                DefaultLockBackingAmount.String(),
		LockMinRepTier:                   DefaultMinRepTierThreadLock,
		MinPostConvictionStake:           DefaultMinPostConvictionStake,
		PostConvictionLockSeconds:        DefaultPostConvictionLockSeconds,
		PostConvictionStreamRatePerBlock: DefaultPostConvictionRepPerDreamPerDay,
		MaxForumRepPerTagPerEpoch:        DefaultMaxForumRepPerTagPerEpoch,
		PostConvictionStakerSlashBps:     DefaultPostConvictionStakerSlashBps,
	}
}

// DefaultSentinelDemotionThresholdAmount is the bond floor below which the
// sentinel transitions from RECOVERY to DEMOTED.
const DefaultSentinelDemotionThresholdAmount = int64(250)

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams()
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if !p.CostPerByteAmount.IsNil() && p.CostPerByteAmount.IsNegative() {
		return fmt.Errorf("cost_per_byte_amount cannot be negative: %s", p.CostPerByteAmount)
	}
	if p.EphemeralTtl <= 0 {
		return fmt.Errorf("ephemeral_ttl must be positive: %d", p.EphemeralTtl)
	}
	if p.MinSentinelTrustLevel != "" {
		if _, ok := reptypes.TrustLevel_value[p.MinSentinelTrustLevel]; !ok {
			return fmt.Errorf("invalid min_sentinel_trust_level: %s", p.MinSentinelTrustLevel)
		}
	}
	if p.MakePermanentMinTrustLevel > 4 {
		return fmt.Errorf("make_permanent_min_trust_level must be 0-4, got %d", p.MakePermanentMinTrustLevel)
	}
	if !p.AuthorRepSlash.IsNil() && p.AuthorRepSlash.IsNegative() {
		return fmt.Errorf("author_rep_slash cannot be negative: %s", p.AuthorRepSlash)
	}
	if !p.MinPostConvictionStake.IsNil() && p.MinPostConvictionStake.IsNegative() {
		return fmt.Errorf("min_post_conviction_stake cannot be negative: %s", p.MinPostConvictionStake)
	}
	if p.PostConvictionLockSeconds < 0 {
		return fmt.Errorf("post_conviction_lock_seconds cannot be negative: %d", p.PostConvictionLockSeconds)
	}
	if !p.PostConvictionStreamRatePerBlock.IsNil() && p.PostConvictionStreamRatePerBlock.IsNegative() {
		return fmt.Errorf("post_conviction_stream_rate_per_block cannot be negative: %s", p.PostConvictionStreamRatePerBlock)
	}
	if !p.MaxForumRepPerTagPerEpoch.IsNil() && p.MaxForumRepPerTagPerEpoch.IsNegative() {
		return fmt.Errorf("max_forum_rep_per_tag_per_epoch cannot be negative: %s", p.MaxForumRepPerTagPerEpoch)
	}
	if p.PostConvictionStakerSlashBps > 10000 {
		return fmt.Errorf("post_conviction_staker_slash_bps must be <= 10000: %d", p.PostConvictionStakerSlashBps)
	}
	if err := validateModerationCaps(p.MaxHidesPerEpoch, p.MaxSentinelLocksPerEpoch, p.MaxSentinelMovesPerEpoch); err != nil {
		return err
	}
	// sentinel_slash_amount: if set, positive and not larger than the base
	// bond (a single action must never reserve more than the whole bond).
	if p.SentinelSlashAmount != "" {
		slash, ok := math.NewIntFromString(p.SentinelSlashAmount)
		if !ok || !slash.IsPositive() {
			return fmt.Errorf("sentinel_slash_amount must be a positive integer: %q", p.SentinelSlashAmount)
		}
		if slash.GT(p.MinSentinelBondOrDefault()) {
			return fmt.Errorf("sentinel_slash_amount (%s) must not exceed min_sentinel_bond (%s)", slash, p.MinSentinelBondOrDefault())
		}
	}
	// lock_backing_amount: if set, positive and at least the derived lock bond.
	if p.LockBackingAmount != "" {
		backing, ok := math.NewIntFromString(p.LockBackingAmount)
		if !ok || !backing.IsPositive() {
			return fmt.Errorf("lock_backing_amount must be a positive integer: %q", p.LockBackingAmount)
		}
		if backing.LT(p.LockMinBondOrDefault()) {
			return fmt.Errorf("lock_backing_amount (%s) must be >= lock bond (%s)", backing, p.LockMinBondOrDefault())
		}
	}
	// lock_min_rep_tier: 0 = unset (default); if set, within [min_sentinel_rep_tier, 5].
	if p.LockMinRepTier != 0 {
		if p.LockMinRepTier > 5 {
			return fmt.Errorf("lock_min_rep_tier must be <= 5: %d", p.LockMinRepTier)
		}
		if p.LockMinRepTier < p.MinSentinelRepTier {
			return fmt.Errorf("lock_min_rep_tier (%d) must be >= min_sentinel_rep_tier (%d)", p.LockMinRepTier, p.MinSentinelRepTier)
		}
	}
	return nil
}

// validateModerationCaps enforces the shared upper bound on the per-action
// epoch caps. A stored 0 is the "unset → default" sentinel and always passes.
func validateModerationCaps(hides, locks, moves uint64) error {
	for name, v := range map[string]uint64{
		"max_hides_per_epoch":          hides,
		"max_sentinel_locks_per_epoch": locks,
		"max_sentinel_moves_per_epoch": moves,
	} {
		if v > ModerationEpochCapCeiling {
			return fmt.Errorf("%s must be <= %d: %d", name, ModerationEpochCapCeiling, v)
		}
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
		BountiesEnabled:                  true,
		ReactionsEnabled:                 true,
		EditingEnabled:                   true,
		SpamTaxAmount:                    DefaultSpamTaxAmount,
		ReactionSpamTaxAmount:            DefaultReactionSpamTaxAmount,
		FlagSpamTaxAmount:                DefaultFlagSpamTaxAmount,
		DownvoteDepositAmount:            DefaultDownvoteDepositAmount,
		AppealFeeAmount:                  DefaultAppealFeeAmount,
		LockAppealFeeAmount:              DefaultLockAppealFeeAmount,
		MoveAppealFeeAmount:              DefaultMoveAppealFeeAmount,
		EditFeeAmount:                    DefaultEditFeeAmount,
		CostPerByteAmount:                DefaultCostPerByteAmount,
		CostPerByteExempt:                false,
		MaxContentSize:                   DefaultMaxContentSize,
		DailyPostLimit:                   DefaultDailyPostLimit,
		MaxReplyDepth:                    DefaultMaxReplyDepth,
		MaxFollowsPerDay:                 DefaultMaxFollowsPerDay,
		BountyCancellationFeePercent:     DefaultBountyCancellationFeePercent,
		EditGracePeriod:                  DefaultEditGracePeriod,
		EditMaxWindow:                    DefaultEditMaxWindow,
		ArchiveThreshold:                 DefaultArchiveThreshold,
		UnarchiveCooldown:                DefaultUnarchiveCooldown,
		ArchiveCooldown:                  DefaultArchiveCooldown,
		HideAppealCooldown:               DefaultHideAppealCooldown,
		LockAppealCooldown:               DefaultLockAppealCooldown,
		MoveAppealCooldown:               DefaultMoveAppealCooldown,
		EphemeralTtl:                     DefaultEphemeralTTL,
		ConvictionRenewalThreshold:       DefaultConvictionRenewalThreshold,
		ConvictionRenewalPeriod:          DefaultConvictionRenewalPeriod,
		MinSentinelBond:                  DefaultMinSentinelBond.String(),
		MinSentinelRepTier:               DefaultMinRepTierSentinel,
		MinSentinelTrustLevel:            "TRUST_LEVEL_ESTABLISHED",
		MinSentinelAgeBlocks:             0,
		SentinelDemotionCooldown:         DefaultSentinelDemotionCooldown,
		SentinelDemotionThreshold:        math.NewInt(DefaultSentinelDemotionThresholdAmount).String(),
		SentinelUnhideWindow:             DefaultSentinelUnhideWindow,
		SentinelUnbondCooldown:           DefaultSentinelUnbondCooldown,
		MakePermanentMinTrustLevel:       DefaultMakePermanentMinTrustLevel,
		MaxPromotionsPerBlock:            DefaultMaxPromotionsPerBlock,
		AuthorRepSlash:                   DefaultAuthorRepSlash,
		MaxMakePermanentPerDay:           DefaultMaxMakePermanentPerDay,
		MaxHidesPerEpoch:                 DefaultMaxHidesPerEpoch,
		MaxSentinelLocksPerEpoch:         DefaultMaxSentinelLocksPerEpoch,
		MaxSentinelMovesPerEpoch:         DefaultMaxSentinelMovesPerEpoch,
		SentinelSlashAmount:              math.NewInt(DefaultSentinelSlashAmount).String(),
		MinPostConvictionStake:           DefaultMinPostConvictionStake,
		PostConvictionLockSeconds:        DefaultPostConvictionLockSeconds,
		PostConvictionStreamRatePerBlock: DefaultPostConvictionRepPerDreamPerDay,
		MaxForumRepPerTagPerEpoch:        DefaultMaxForumRepPerTagPerEpoch,
		PostConvictionStakerSlashBps:     DefaultPostConvictionStakerSlashBps,
	}
}

// Validate validates the operational parameters.
func (p ForumOperationalParams) Validate() error {
	if p.EphemeralTtl <= 0 {
		return fmt.Errorf("ephemeral_ttl must be positive: %d", p.EphemeralTtl)
	}
	if !p.CostPerByteAmount.IsNil() && p.CostPerByteAmount.IsNegative() {
		return fmt.Errorf("cost_per_byte_amount cannot be negative: %s", p.CostPerByteAmount)
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
	if p.MinSentinelTrustLevel != "" {
		if _, ok := reptypes.TrustLevel_value[p.MinSentinelTrustLevel]; !ok {
			return fmt.Errorf("invalid min_sentinel_trust_level: %s", p.MinSentinelTrustLevel)
		}
	}
	if p.MakePermanentMinTrustLevel > 4 {
		return fmt.Errorf("make_permanent_min_trust_level must be 0-4, got %d", p.MakePermanentMinTrustLevel)
	}
	if !p.AuthorRepSlash.IsNil() && p.AuthorRepSlash.IsNegative() {
		return fmt.Errorf("author_rep_slash cannot be negative: %s", p.AuthorRepSlash)
	}
	if !p.MinPostConvictionStake.IsNil() && p.MinPostConvictionStake.IsNegative() {
		return fmt.Errorf("min_post_conviction_stake cannot be negative: %s", p.MinPostConvictionStake)
	}
	if p.PostConvictionLockSeconds < 0 {
		return fmt.Errorf("post_conviction_lock_seconds cannot be negative: %d", p.PostConvictionLockSeconds)
	}
	if !p.PostConvictionStreamRatePerBlock.IsNil() && p.PostConvictionStreamRatePerBlock.IsNegative() {
		return fmt.Errorf("post_conviction_stream_rate_per_block cannot be negative: %s", p.PostConvictionStreamRatePerBlock)
	}
	if !p.MaxForumRepPerTagPerEpoch.IsNil() && p.MaxForumRepPerTagPerEpoch.IsNegative() {
		return fmt.Errorf("max_forum_rep_per_tag_per_epoch cannot be negative: %s", p.MaxForumRepPerTagPerEpoch)
	}
	if p.PostConvictionStakerSlashBps > 10000 {
		return fmt.Errorf("post_conviction_staker_slash_bps must be <= 10000: %d", p.PostConvictionStakerSlashBps)
	}
	if err := validateModerationCaps(p.MaxHidesPerEpoch, p.MaxSentinelLocksPerEpoch, p.MaxSentinelMovesPerEpoch); err != nil {
		return err
	}
	// sentinel_slash_amount: if set, positive and not larger than the base bond.
	// The full invariant is re-checked against the merged Params after apply.
	if p.SentinelSlashAmount != "" {
		slash, ok := math.NewIntFromString(p.SentinelSlashAmount)
		if !ok || !slash.IsPositive() {
			return fmt.Errorf("sentinel_slash_amount must be a positive integer: %q", p.SentinelSlashAmount)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Moderation-parameter accessors. Each returns the stored value, or the
// compile-time default when the stored value is unset (0 / empty / unparseable).
// This zero-means-default convention lets a chain that adds these fields via an
// upgrade keep today's behavior with NO state migration (proto3 reads absent
// fields as 0); fresh genesis seeds the real defaults explicitly.
// ---------------------------------------------------------------------------

// MaxHidesPerEpochOrDefault returns the effective per-epoch hide cap.
func (p Params) MaxHidesPerEpochOrDefault() uint64 {
	if p.MaxHidesPerEpoch == 0 {
		return DefaultMaxHidesPerEpoch
	}
	return p.MaxHidesPerEpoch
}

// MaxSentinelLocksPerEpochOrDefault returns the effective per-epoch lock cap.
func (p Params) MaxSentinelLocksPerEpochOrDefault() uint64 {
	if p.MaxSentinelLocksPerEpoch == 0 {
		return DefaultMaxSentinelLocksPerEpoch
	}
	return p.MaxSentinelLocksPerEpoch
}

// MaxSentinelMovesPerEpochOrDefault returns the effective per-epoch move cap.
func (p Params) MaxSentinelMovesPerEpochOrDefault() uint64 {
	if p.MaxSentinelMovesPerEpoch == 0 {
		return DefaultMaxSentinelMovesPerEpoch
	}
	return p.MaxSentinelMovesPerEpoch
}

// SentinelSlashAmountOrDefault returns the effective per-action slash (udream).
func (p Params) SentinelSlashAmountOrDefault() math.Int {
	if v, ok := math.NewIntFromString(p.SentinelSlashAmount); ok && v.IsPositive() {
		return v
	}
	return math.NewInt(DefaultSentinelSlashAmount)
}

// MinSentinelBondOrDefault returns the effective base sentinel bond (udream).
func (p Params) MinSentinelBondOrDefault() math.Int {
	if v, ok := math.NewIntFromString(p.MinSentinelBond); ok && v.IsPositive() {
		return v
	}
	return DefaultMinSentinelBond
}

// LockBondMultiplierOrDefault returns the effective lock-bond multiplier (>= 1).
func (p Params) LockBondMultiplierOrDefault() uint64 {
	if p.LockBondMultiplier == 0 {
		return DefaultLockBondMultiplier
	}
	return p.LockBondMultiplier
}

// LockMinBondOrDefault returns the minimum bond required to lock a thread,
// derived as lock_bond_multiplier × min_sentinel_bond — one source of truth so
// the lock floor can never silently diverge from the base bond.
func (p Params) LockMinBondOrDefault() math.Int {
	return p.MinSentinelBondOrDefault().Mul(math.NewIntFromUint64(p.LockBondMultiplierOrDefault()))
}

// LockBackingAmountOrDefault returns the minimum DREAM backing to lock (udream).
func (p Params) LockBackingAmountOrDefault() math.Int {
	if v, ok := math.NewIntFromString(p.LockBackingAmount); ok && v.IsPositive() {
		return v
	}
	return DefaultLockBackingAmount
}

// LockMinRepTierOrDefault returns the minimum rep tier required to lock a thread.
func (p Params) LockMinRepTierOrDefault() uint64 {
	if p.LockMinRepTier == 0 {
		return DefaultMinRepTierThreadLock
	}
	return p.LockMinRepTier
}

// ApplyOperationalParams copies all operational fields from ForumOperationalParams
// onto the full Params, preserving non-operational fields (forum_paused, moderation_paused, appeals_paused).
func (p Params) ApplyOperationalParams(op ForumOperationalParams) Params {
	p.BountiesEnabled = op.BountiesEnabled
	p.ReactionsEnabled = op.ReactionsEnabled
	p.EditingEnabled = op.EditingEnabled
	p.SpamTaxAmount = op.SpamTaxAmount
	p.ReactionSpamTaxAmount = op.ReactionSpamTaxAmount
	p.FlagSpamTaxAmount = op.FlagSpamTaxAmount
	p.DownvoteDepositAmount = op.DownvoteDepositAmount
	p.AppealFeeAmount = op.AppealFeeAmount
	p.LockAppealFeeAmount = op.LockAppealFeeAmount
	p.MoveAppealFeeAmount = op.MoveAppealFeeAmount
	p.EditFeeAmount = op.EditFeeAmount
	p.CostPerByteAmount = op.CostPerByteAmount
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
	p.ConvictionRenewalThreshold = op.ConvictionRenewalThreshold
	p.ConvictionRenewalPeriod = op.ConvictionRenewalPeriod
	p.MinSentinelBond = op.MinSentinelBond
	p.MinSentinelRepTier = op.MinSentinelRepTier
	p.MinSentinelTrustLevel = op.MinSentinelTrustLevel
	p.MinSentinelAgeBlocks = op.MinSentinelAgeBlocks
	p.SentinelDemotionCooldown = op.SentinelDemotionCooldown
	p.SentinelDemotionThreshold = op.SentinelDemotionThreshold
	p.SentinelUnhideWindow = op.SentinelUnhideWindow
	// Zero is a meaningful value (immediate withdrawal), so always copy.
	p.SentinelUnbondCooldown = op.SentinelUnbondCooldown
	p.MakePermanentMinTrustLevel = op.MakePermanentMinTrustLevel
	p.MaxPromotionsPerBlock = op.MaxPromotionsPerBlock
	p.AuthorRepSlash = op.AuthorRepSlash
	p.MaxMakePermanentPerDay = op.MaxMakePermanentPerDay
	p.MaxHidesPerEpoch = op.MaxHidesPerEpoch
	p.MaxSentinelLocksPerEpoch = op.MaxSentinelLocksPerEpoch
	p.MaxSentinelMovesPerEpoch = op.MaxSentinelMovesPerEpoch
	p.SentinelSlashAmount = op.SentinelSlashAmount
	// NB: lock_bond_multiplier / lock_backing_amount / lock_min_rep_tier are
	// governance-only (Params) and are deliberately NOT copied from the
	// operational params, so an Operations-Committee update leaves them intact.
	p.MinPostConvictionStake = op.MinPostConvictionStake
	p.PostConvictionLockSeconds = op.PostConvictionLockSeconds
	p.PostConvictionStreamRatePerBlock = op.PostConvictionStreamRatePerBlock
	p.MaxForumRepPerTagPerEpoch = op.MaxForumRepPerTagPerEpoch
	p.PostConvictionStakerSlashBps = op.PostConvictionStakerSlashBps
	return p
}

// ExtractOperationalParams extracts the operational fields from Params into ForumOperationalParams.
func (p Params) ExtractOperationalParams() ForumOperationalParams {
	return ForumOperationalParams{
		BountiesEnabled:                  p.BountiesEnabled,
		ReactionsEnabled:                 p.ReactionsEnabled,
		EditingEnabled:                   p.EditingEnabled,
		SpamTaxAmount:                    p.SpamTaxAmount,
		ReactionSpamTaxAmount:            p.ReactionSpamTaxAmount,
		FlagSpamTaxAmount:                p.FlagSpamTaxAmount,
		DownvoteDepositAmount:            p.DownvoteDepositAmount,
		AppealFeeAmount:                  p.AppealFeeAmount,
		LockAppealFeeAmount:              p.LockAppealFeeAmount,
		MoveAppealFeeAmount:              p.MoveAppealFeeAmount,
		EditFeeAmount:                    p.EditFeeAmount,
		CostPerByteAmount:                p.CostPerByteAmount,
		CostPerByteExempt:                p.CostPerByteExempt,
		MaxContentSize:                   p.MaxContentSize,
		DailyPostLimit:                   p.DailyPostLimit,
		MaxReplyDepth:                    p.MaxReplyDepth,
		MaxFollowsPerDay:                 p.MaxFollowsPerDay,
		BountyCancellationFeePercent:     p.BountyCancellationFeePercent,
		EditGracePeriod:                  p.EditGracePeriod,
		EditMaxWindow:                    p.EditMaxWindow,
		ArchiveThreshold:                 p.ArchiveThreshold,
		UnarchiveCooldown:                p.UnarchiveCooldown,
		ArchiveCooldown:                  p.ArchiveCooldown,
		HideAppealCooldown:               p.HideAppealCooldown,
		LockAppealCooldown:               p.LockAppealCooldown,
		MoveAppealCooldown:               p.MoveAppealCooldown,
		EphemeralTtl:                     p.EphemeralTtl,
		ConvictionRenewalThreshold:       p.ConvictionRenewalThreshold,
		ConvictionRenewalPeriod:          p.ConvictionRenewalPeriod,
		MinSentinelBond:                  p.MinSentinelBond,
		MinSentinelRepTier:               p.MinSentinelRepTier,
		MinSentinelTrustLevel:            p.MinSentinelTrustLevel,
		MinSentinelAgeBlocks:             p.MinSentinelAgeBlocks,
		SentinelDemotionCooldown:         p.SentinelDemotionCooldown,
		SentinelDemotionThreshold:        p.SentinelDemotionThreshold,
		SentinelUnhideWindow:             p.SentinelUnhideWindow,
		SentinelUnbondCooldown:           p.SentinelUnbondCooldown,
		MakePermanentMinTrustLevel:       p.MakePermanentMinTrustLevel,
		MaxPromotionsPerBlock:            p.MaxPromotionsPerBlock,
		AuthorRepSlash:                   p.AuthorRepSlash,
		MaxMakePermanentPerDay:           p.MaxMakePermanentPerDay,
		MaxHidesPerEpoch:                 p.MaxHidesPerEpoch,
		MaxSentinelLocksPerEpoch:         p.MaxSentinelLocksPerEpoch,
		MaxSentinelMovesPerEpoch:         p.MaxSentinelMovesPerEpoch,
		SentinelSlashAmount:              p.SentinelSlashAmount,
		MinPostConvictionStake:           p.MinPostConvictionStake,
		PostConvictionLockSeconds:        p.PostConvictionLockSeconds,
		PostConvictionStreamRatePerBlock: p.PostConvictionStreamRatePerBlock,
		MaxForumRepPerTagPerEpoch:        p.MaxForumRepPerTagPerEpoch,
		PostConvictionStakerSlashBps:     p.PostConvictionStakerSlashBps,
	}
}

// SentinelBondedRoleConfig assembles a reptypes-agnostic BondedRoleConfig from
// forum's operational params. Kept in params.go so the type is accessible from
// both the keeper (which write-throughs on update) and InitGenesis.
func (p Params) SentinelBondedRoleConfigFields() (minBond, trust, demotionThreshold string, repTier uint64, ageBlocks, demotionCooldown, unbondCooldown int64) {
	return p.MinSentinelBond, p.MinSentinelTrustLevel, p.SentinelDemotionThreshold,
		p.MinSentinelRepTier, p.MinSentinelAgeBlocks, p.SentinelDemotionCooldown, p.SentinelUnbondCooldown
}
