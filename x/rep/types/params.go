package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// DefaultParams returns a default set of parameters.
// PRODUCTION values - use config.yml to override for testing/development.
func DefaultParams() Params {
	return Params{
		// Time - PRODUCTION values
		EpochBlocks:          14400, // ~1 day (14400 blocks * 6s = 86400s = 1 day)
		SeasonDurationEpochs: 150,   // ~5 months (150 days)

		// DREAM economics
		StakingApy:         math.LegacyNewDecWithPrec(10, 2), // 10%
		UnstakedDecayRate:  math.LegacyNewDecWithPrec(1, 2),  // 1%
		TransferTaxRate:    math.LegacyNewDecWithPrec(3, 2),  // 3%
		MaxTipAmount:       math.NewInt(100000000),           // 100 DREAM (100 * 1e6 micro-DREAM)
		MaxTipsPerEpoch:    10,
		MaxGiftAmount:      math.NewInt(500000000), // 500 DREAM (500 * 1e6 micro-DREAM)
		GiftOnlyToInvitees: true,

		// Initiative rewards
		CompleterShare:          math.LegacyNewDecWithPrec(90, 2), // 90%
		TreasuryShare:           math.LegacyNewDecWithPrec(10, 2), // 10%
		MinReputationMultiplier: math.LegacyNewDecWithPrec(10, 2), // 10%

		// Initiative tiers (MaxBudget in micro-DREAM: 1 DREAM = 1,000,000 micro-DREAM)
		ApprenticeTier: TierConfig{
			MaxBudget:        math.NewInt(100000000), // 100 DREAM
			MinReputation:    math.LegacyZeroDec(),
			ReputationCap:    math.LegacyNewDec(25),
			RewardMultiplier: math.LegacyNewDecWithPrec(50, 2), // 0.5x
		},
		StandardTier: TierConfig{
			MaxBudget:        math.NewInt(500000000), // 500 DREAM
			MinReputation:    math.LegacyNewDec(25),
			ReputationCap:    math.LegacyNewDec(100),
			RewardMultiplier: math.LegacyOneDec(), // 1.0x
		},
		ExpertTier: TierConfig{
			MaxBudget:        math.NewInt(2000000000), // 2000 DREAM
			MinReputation:    math.LegacyNewDec(100),
			ReputationCap:    math.LegacyNewDec(500),
			RewardMultiplier: math.LegacyNewDecWithPrec(150, 2), // 1.5x
		},
		EpicTier: TierConfig{
			MaxBudget:        math.NewInt(10000000000), // 10000 DREAM
			MinReputation:    math.LegacyNewDec(250),
			ReputationCap:    math.LegacyNewDec(1000),
			RewardMultiplier: math.LegacyNewDec(2), // 2.0x
		},

		// Conviction - PRODUCTION values
		// FIXED: ConvictionPerDream with sqrt scaling on both sides
		// Formula: required_conviction = ConvictionPerDream × sqrt(budget)
		//          actual_conviction = sqrt(total_stakes × time × rep)
		// This maintains constant ~4% stake-to-budget ratio across ALL budget sizes
		// Example: 100 DREAM → need 4 DREAM, 10K DREAM → need 400 DREAM
		ConvictionHalfLifeEpochs: 7,                                // 7 epochs = 7 days half-life
		ExternalConvictionRatio:  math.LegacyNewDecWithPrec(50, 2), // 50%
		ConvictionPerDream:       math.LegacyNewDecWithPrec(20, 2), // 0.2 (sqrt scaling)

		// Review periods - PRODUCTION values
		DefaultReviewPeriodEpochs:    7, // 7 epochs = ~1 week
		DefaultChallengePeriodEpochs: 7, // 7 epochs = ~1 week

		// Invitations - PRODUCTION values
		MinInvitationStake:             math.NewInt(100),
		InvitationAccountabilityEpochs: 150,                               // 150 epochs = ~5 months (1 season)
		ReferralRewardRate:             math.LegacyNewDecWithPrec(5, 2),   // 5%
		InvitationCostMultiplier:       math.LegacyNewDecWithPrec(110, 2), // 1.1x

		// Trust levels configuration
		// NOTE: TrustLevelConfig values are hardcoded here because Ignite's YAML parser
		// cannot handle nested proto message structures in config.yml. To switch between
		// production and testing values, comment/uncomment the appropriate section.
		// See x/commons/keeper/genesis_vals.go for the same pattern.
		TrustLevelConfig: getTrustLevelConfig(),

		// Challenges
		MinChallengeStake:    math.NewInt(50),
		ChallengerRewardRate: math.LegacyNewDecWithPrec(20, 2), // 20%
		JurySize:             5,
		JurySuperMajority:    math.LegacyNewDecWithPrec(67, 2), // 67%
		MinJurorReputation:   math.LegacyNewDec(50),

		// Interim compensation - PRODUCTION values (in micro-DREAM: 1 DREAM = 1e6 micro-DREAM)
		SimpleComplexityBudget:   math.NewInt(50000000),            // 50 DREAM
		StandardComplexityBudget: math.NewInt(150000000),           // 150 DREAM
		ComplexComplexityBudget:  math.NewInt(400000000),           // 400 DREAM
		ExpertComplexityBudget:   math.NewInt(1000000000),          // 1000 DREAM
		SoloExpertBonusRate:      math.LegacyNewDecWithPrec(50, 2), // 50%
		InterimDeadlineEpochs:    7,                                // 7 epochs = ~1 week

		// Rate limits
		MaxActiveChallengesPerCommittee: 3,
		MaxNewChallengesPerEpoch:        2,
		ChallengeQueueMaxSize:           10,

		// Slashing
		MinorSlashPenalty:    math.LegacyNewDecWithPrec(5, 2),  // 5%
		ModerateSlashPenalty: math.LegacyNewDecWithPrec(15, 2), // 15%
		SevereSlashPenalty:   math.LegacyNewDecWithPrec(30, 2), // 30%
		ZeroingSlashPenalty:  math.LegacyOneDec(),              // 100%

		// Extended staking (project/member/tag)
		ProjectStakingApy:          math.LegacyNewDecWithPrec(8, 2), // 8% APY while project is active
		ProjectCompletionBonusRate: math.LegacyNewDecWithPrec(5, 2), // 5% completion bonus
		MemberStakeRevenueShare:    math.LegacyNewDecWithPrec(5, 2), // 5% revenue share to member stakers
		TagStakeRevenueShare:       math.LegacyNewDecWithPrec(2, 2), // 2% per tag revenue share
		MinStakeDurationSeconds:    86400,                           // 24 hours minimum
		AllowSelfMemberStake:       false,                           // Cannot stake on yourself

		// Challenge response deadline - PRODUCTION values
		ChallengeResponseDeadlineEpochs: 3, // 3 epochs = ~3 days

		// Gift rate limiting - PRODUCTION values
		GiftCooldownBlocks:     14400,                   // 1 day (14400 blocks * 6s = 86400s = 1 day)
		MaxGiftsPerSenderEpoch: math.NewInt(2000000000), // 2000 DREAM per epoch total (2000 * 1e6 micro-DREAM)

		// Content conviction staking
		ContentConvictionHalfLifeEpochs: 14,                       // 14 epochs = ~2 weeks (slower than initiative conviction)
		MaxContentStakePerMember:        math.NewInt(10000000000), // 10,000 DREAM per member per content item
		MaxAuthorBondPerContent:         math.NewInt(1000000000),  // 1,000 DREAM max author bond per content item
		AuthorBondSlashOnModeration:     true,                     // Slash author bonds when content is moderated

		// Content challenge reward share (fraction of slashed bond minted to challenger)
		ContentChallengeRewardShare: math.LegacyNewDecWithPrec(50, 2), // 50%

		// Conviction propagation (fraction of linked content conviction propagated to initiative)
		ConvictionPropagationRatio: math.LegacyNewDecWithPrec(10, 2), // 10%

		// Tag anti-gaming
		MaxTagsPerInitiative: 3, // Max 3 tags per initiative (prevents tag stuffing for rep/revenue inflation)

		// Anti-gaming parameters
		ReputationDecayRate:         math.LegacyNewDecWithPrec(5, 3),  // 0.5% per epoch (~47% retained over a 5-month season)
		MaxConvictionSharePerMember: math.LegacyNewDecWithPrec(33, 2), // 33% — no single member can contribute more than 1/3 of required conviction
		InvitationStakeBurnRate:     math.LegacyNewDecWithPrec(10, 2), // 10% of invitation stake burned on acceptance
		MaxReputationGainPerEpoch:   math.LegacyNewDec(50),            // Max 50 reputation per tag per epoch (prevents interim grinding)
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	// Time validation
	if p.EpochBlocks <= 0 {
		return fmt.Errorf("epoch blocks must be positive: %d", p.EpochBlocks)
	}
	if p.SeasonDurationEpochs <= 0 {
		return fmt.Errorf("season duration epochs must be positive: %d", p.SeasonDurationEpochs)
	}

	// DREAM economics validation
	if p.UnstakedDecayRate.IsNegative() {
		return fmt.Errorf("decay rate cannot be negative: %s", p.UnstakedDecayRate)
	}
	if p.UnstakedDecayRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("decay rate cannot be greater than 1: %s", p.UnstakedDecayRate)
	}

	// Shares must sum to 1
	totalShare := p.CompleterShare.Add(p.TreasuryShare)
	if !totalShare.Equal(math.LegacyOneDec()) {
		return fmt.Errorf("completer and treasury shares must sum to 1: %s", totalShare)
	}

	// Tier validation
	if p.ApprenticeTier.MaxBudget.IsNegative() || p.StandardTier.MaxBudget.IsNegative() ||
		p.ExpertTier.MaxBudget.IsNegative() || p.EpicTier.MaxBudget.IsNegative() {
		return fmt.Errorf("tier max budgets must be non-negative")
	}

	// Jury size must be odd for tiebreaking
	if p.JurySize%2 == 0 {
		return fmt.Errorf("jury size must be odd: %d", p.JurySize)
	}

	// Gift rate limiting validation
	if p.GiftCooldownBlocks < 0 {
		return fmt.Errorf("gift cooldown blocks cannot be negative: %d", p.GiftCooldownBlocks)
	}
	if p.MaxGiftsPerSenderEpoch.IsNegative() {
		return fmt.Errorf("max gifts per sender epoch cannot be negative: %s", p.MaxGiftsPerSenderEpoch)
	}

	// Content conviction staking validation
	if p.ContentConvictionHalfLifeEpochs <= 0 {
		return fmt.Errorf("content conviction half life epochs must be positive: %d", p.ContentConvictionHalfLifeEpochs)
	}
	if !p.MaxContentStakePerMember.IsPositive() {
		return fmt.Errorf("max content stake per member must be positive: %s", p.MaxContentStakePerMember)
	}
	if !p.MaxAuthorBondPerContent.IsPositive() {
		return fmt.Errorf("max author bond per content must be positive: %s", p.MaxAuthorBondPerContent)
	}

	// Content challenge reward share validation
	if p.ContentChallengeRewardShare.IsNegative() {
		return fmt.Errorf("content challenge reward share cannot be negative: %s", p.ContentChallengeRewardShare)
	}
	if p.ContentChallengeRewardShare.GT(math.LegacyOneDec()) {
		return fmt.Errorf("content challenge reward share cannot be greater than 1: %s", p.ContentChallengeRewardShare)
	}

	// Conviction propagation ratio validation
	if p.ConvictionPropagationRatio.IsNegative() {
		return fmt.Errorf("conviction propagation ratio cannot be negative: %s", p.ConvictionPropagationRatio)
	}
	if p.ConvictionPropagationRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("conviction propagation ratio cannot be greater than 1: %s", p.ConvictionPropagationRatio)
	}

	// Anti-gaming parameter validation
	if p.ReputationDecayRate.IsNegative() {
		return fmt.Errorf("reputation decay rate cannot be negative: %s", p.ReputationDecayRate)
	}
	if p.ReputationDecayRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("reputation decay rate cannot be greater than 1: %s", p.ReputationDecayRate)
	}
	if p.MaxConvictionSharePerMember.IsNegative() || p.MaxConvictionSharePerMember.IsZero() {
		return fmt.Errorf("max conviction share per member must be positive: %s", p.MaxConvictionSharePerMember)
	}
	if p.MaxConvictionSharePerMember.GT(math.LegacyOneDec()) {
		return fmt.Errorf("max conviction share per member cannot be greater than 1: %s", p.MaxConvictionSharePerMember)
	}
	if p.InvitationStakeBurnRate.IsNegative() {
		return fmt.Errorf("invitation stake burn rate cannot be negative: %s", p.InvitationStakeBurnRate)
	}
	if p.InvitationStakeBurnRate.GTE(math.LegacyOneDec()) {
		return fmt.Errorf("invitation stake burn rate must be less than 1: %s", p.InvitationStakeBurnRate)
	}
	if p.MaxReputationGainPerEpoch.IsNegative() {
		return fmt.Errorf("max reputation gain per epoch cannot be negative: %s", p.MaxReputationGainPerEpoch)
	}

	// Tag anti-gaming
	if p.MaxTagsPerInitiative == 0 {
		return fmt.Errorf("max tags per initiative must be positive")
	}

	return nil
}

// DefaultRepOperationalParams returns default operational parameters.
func DefaultRepOperationalParams() RepOperationalParams {
	return RepOperationalParams{
		// Time
		EpochBlocks:          14400,
		SeasonDurationEpochs: 150,
		// DREAM economics
		StakingApy:         math.LegacyNewDecWithPrec(10, 2), // 10%
		UnstakedDecayRate:  math.LegacyNewDecWithPrec(1, 2),  // 1%
		TransferTaxRate:    math.LegacyNewDecWithPrec(3, 2),  // 3%
		MaxTipAmount:       math.NewInt(100000000),           // 100 DREAM
		MaxTipsPerEpoch:    10,
		MaxGiftAmount:      math.NewInt(500000000), // 500 DREAM
		GiftOnlyToInvitees: true,
		// Reputation
		MinReputationMultiplier: math.LegacyNewDecWithPrec(10, 2), // 10%
		// Review periods
		DefaultReviewPeriodEpochs:    7,
		DefaultChallengePeriodEpochs: 7,
		// Invitations
		MinInvitationStake:             math.NewInt(100),
		InvitationAccountabilityEpochs: 150,
		ReferralRewardRate:             math.LegacyNewDecWithPrec(5, 2),   // 5%
		InvitationCostMultiplier:       math.LegacyNewDecWithPrec(110, 2), // 1.1x
		// Challenges
		MinChallengeStake:    math.NewInt(50),
		ChallengerRewardRate: math.LegacyNewDecWithPrec(20, 2), // 20%
		JurySize:             5,
		JurySuperMajority:    math.LegacyNewDecWithPrec(67, 2), // 67%
		MinJurorReputation:   math.LegacyNewDec(50),
		// Interim compensation
		SimpleComplexityBudget:   math.NewInt(50000000),            // 50 DREAM
		StandardComplexityBudget: math.NewInt(150000000),           // 150 DREAM
		ComplexComplexityBudget:  math.NewInt(400000000),           // 400 DREAM
		ExpertComplexityBudget:   math.NewInt(1000000000),          // 1000 DREAM
		SoloExpertBonusRate:      math.LegacyNewDecWithPrec(50, 2), // 50%
		InterimDeadlineEpochs:    7,
		// Rate limits
		MaxActiveChallengesPerCommittee: 3,
		MaxNewChallengesPerEpoch:        2,
		ChallengeQueueMaxSize:           10,
		// Extended staking
		ProjectStakingApy:          math.LegacyNewDecWithPrec(8, 2), // 8%
		ProjectCompletionBonusRate: math.LegacyNewDecWithPrec(5, 2), // 5%
		MemberStakeRevenueShare:    math.LegacyNewDecWithPrec(5, 2), // 5%
		TagStakeRevenueShare:       math.LegacyNewDecWithPrec(2, 2), // 2%
		MinStakeDurationSeconds:    86400,                           // 24 hours
		AllowSelfMemberStake:       false,
		// Challenge response deadline
		ChallengeResponseDeadlineEpochs: 3,
		// Gift rate limiting
		GiftCooldownBlocks:     14400,
		MaxGiftsPerSenderEpoch: math.NewInt(2000000000), // 2000 DREAM
		// Content conviction staking
		ContentConvictionHalfLifeEpochs: 14,
		MaxContentStakePerMember:        math.NewInt(10000000000), // 10,000 DREAM
		MaxAuthorBondPerContent:         math.NewInt(1000000000),  // 1,000 DREAM
		AuthorBondSlashOnModeration:     true,
		// Content challenge reward share
		ContentChallengeRewardShare: math.LegacyNewDecWithPrec(50, 2), // 50%
		// Conviction propagation
		ConvictionPropagationRatio: math.LegacyNewDecWithPrec(10, 2), // 10%
		// Tag anti-gaming
		MaxTagsPerInitiative: 3,
		// Anti-gaming
		ReputationDecayRate:         math.LegacyNewDecWithPrec(5, 3),  // 0.5% per epoch
		MaxConvictionSharePerMember: math.LegacyNewDecWithPrec(33, 2), // 33%
		InvitationStakeBurnRate:     math.LegacyNewDecWithPrec(10, 2), // 10%
		MaxReputationGainPerEpoch:   math.LegacyNewDec(50),            // Max 50 per tag per epoch
	}
}

// Validate validates the operational parameters.
func (op RepOperationalParams) Validate() error {
	if op.EpochBlocks <= 0 {
		return fmt.Errorf("epoch blocks must be positive: %d", op.EpochBlocks)
	}
	if op.SeasonDurationEpochs <= 0 {
		return fmt.Errorf("season duration epochs must be positive: %d", op.SeasonDurationEpochs)
	}
	if op.UnstakedDecayRate.IsNegative() {
		return fmt.Errorf("unstaked decay rate cannot be negative: %s", op.UnstakedDecayRate)
	}
	if op.UnstakedDecayRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("unstaked decay rate cannot be greater than 1: %s", op.UnstakedDecayRate)
	}
	if op.TransferTaxRate.IsNegative() {
		return fmt.Errorf("transfer tax rate cannot be negative: %s", op.TransferTaxRate)
	}
	if op.TransferTaxRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("transfer tax rate cannot be greater than 1: %s", op.TransferTaxRate)
	}
	if op.JurySize%2 == 0 {
		return fmt.Errorf("jury size must be odd: %d", op.JurySize)
	}
	if op.GiftCooldownBlocks < 0 {
		return fmt.Errorf("gift cooldown blocks cannot be negative: %d", op.GiftCooldownBlocks)
	}
	if op.MaxGiftsPerSenderEpoch.IsNegative() {
		return fmt.Errorf("max gifts per sender epoch cannot be negative: %s", op.MaxGiftsPerSenderEpoch)
	}
	// Content conviction staking validation
	if op.ContentConvictionHalfLifeEpochs <= 0 {
		return fmt.Errorf("content conviction half life epochs must be positive: %d", op.ContentConvictionHalfLifeEpochs)
	}
	if !op.MaxContentStakePerMember.IsPositive() {
		return fmt.Errorf("max content stake per member must be positive: %s", op.MaxContentStakePerMember)
	}
	if !op.MaxAuthorBondPerContent.IsPositive() {
		return fmt.Errorf("max author bond per content must be positive: %s", op.MaxAuthorBondPerContent)
	}
	// Content challenge reward share validation
	if op.ContentChallengeRewardShare.IsNegative() {
		return fmt.Errorf("content challenge reward share cannot be negative: %s", op.ContentChallengeRewardShare)
	}
	if op.ContentChallengeRewardShare.GT(math.LegacyOneDec()) {
		return fmt.Errorf("content challenge reward share cannot be greater than 1: %s", op.ContentChallengeRewardShare)
	}
	// Conviction propagation ratio validation
	if op.ConvictionPropagationRatio.IsNegative() {
		return fmt.Errorf("conviction propagation ratio cannot be negative: %s", op.ConvictionPropagationRatio)
	}
	if op.ConvictionPropagationRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("conviction propagation ratio cannot be greater than 1: %s", op.ConvictionPropagationRatio)
	}
	// Anti-gaming parameter validation
	if op.ReputationDecayRate.IsNegative() {
		return fmt.Errorf("reputation decay rate cannot be negative: %s", op.ReputationDecayRate)
	}
	if op.ReputationDecayRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("reputation decay rate cannot be greater than 1: %s", op.ReputationDecayRate)
	}
	if op.MaxConvictionSharePerMember.IsNegative() || op.MaxConvictionSharePerMember.IsZero() {
		return fmt.Errorf("max conviction share per member must be positive: %s", op.MaxConvictionSharePerMember)
	}
	if op.MaxConvictionSharePerMember.GT(math.LegacyOneDec()) {
		return fmt.Errorf("max conviction share per member cannot be greater than 1: %s", op.MaxConvictionSharePerMember)
	}
	if op.InvitationStakeBurnRate.IsNegative() {
		return fmt.Errorf("invitation stake burn rate cannot be negative: %s", op.InvitationStakeBurnRate)
	}
	if op.InvitationStakeBurnRate.GTE(math.LegacyOneDec()) {
		return fmt.Errorf("invitation stake burn rate must be less than 1: %s", op.InvitationStakeBurnRate)
	}
	if op.MaxReputationGainPerEpoch.IsNegative() {
		return fmt.Errorf("max reputation gain per epoch cannot be negative: %s", op.MaxReputationGainPerEpoch)
	}
	// Tag anti-gaming
	if op.MaxTagsPerInitiative == 0 {
		return fmt.Errorf("max tags per initiative must be positive")
	}
	return nil
}

// ApplyOperationalParams copies all operational fields from RepOperationalParams
// onto the full Params, preserving governance-only fields.
func (p Params) ApplyOperationalParams(op RepOperationalParams) Params {
	// Time
	p.EpochBlocks = op.EpochBlocks
	p.SeasonDurationEpochs = op.SeasonDurationEpochs
	// DREAM economics
	p.StakingApy = op.StakingApy
	p.UnstakedDecayRate = op.UnstakedDecayRate
	p.TransferTaxRate = op.TransferTaxRate
	p.MaxTipAmount = op.MaxTipAmount
	p.MaxTipsPerEpoch = op.MaxTipsPerEpoch
	p.MaxGiftAmount = op.MaxGiftAmount
	p.GiftOnlyToInvitees = op.GiftOnlyToInvitees
	// Reputation
	p.MinReputationMultiplier = op.MinReputationMultiplier
	// Review periods
	p.DefaultReviewPeriodEpochs = op.DefaultReviewPeriodEpochs
	p.DefaultChallengePeriodEpochs = op.DefaultChallengePeriodEpochs
	// Invitations
	p.MinInvitationStake = op.MinInvitationStake
	p.InvitationAccountabilityEpochs = op.InvitationAccountabilityEpochs
	p.ReferralRewardRate = op.ReferralRewardRate
	p.InvitationCostMultiplier = op.InvitationCostMultiplier
	// Challenges
	p.MinChallengeStake = op.MinChallengeStake
	p.ChallengerRewardRate = op.ChallengerRewardRate
	p.JurySize = op.JurySize
	p.JurySuperMajority = op.JurySuperMajority
	p.MinJurorReputation = op.MinJurorReputation
	// Interim compensation
	p.SimpleComplexityBudget = op.SimpleComplexityBudget
	p.StandardComplexityBudget = op.StandardComplexityBudget
	p.ComplexComplexityBudget = op.ComplexComplexityBudget
	p.ExpertComplexityBudget = op.ExpertComplexityBudget
	p.SoloExpertBonusRate = op.SoloExpertBonusRate
	p.InterimDeadlineEpochs = op.InterimDeadlineEpochs
	// Rate limits
	p.MaxActiveChallengesPerCommittee = op.MaxActiveChallengesPerCommittee
	p.MaxNewChallengesPerEpoch = op.MaxNewChallengesPerEpoch
	p.ChallengeQueueMaxSize = op.ChallengeQueueMaxSize
	// Extended staking
	p.ProjectStakingApy = op.ProjectStakingApy
	p.ProjectCompletionBonusRate = op.ProjectCompletionBonusRate
	p.MemberStakeRevenueShare = op.MemberStakeRevenueShare
	p.TagStakeRevenueShare = op.TagStakeRevenueShare
	p.MinStakeDurationSeconds = op.MinStakeDurationSeconds
	p.AllowSelfMemberStake = op.AllowSelfMemberStake
	// Challenge response deadline
	p.ChallengeResponseDeadlineEpochs = op.ChallengeResponseDeadlineEpochs
	// Gift rate limiting
	p.GiftCooldownBlocks = op.GiftCooldownBlocks
	p.MaxGiftsPerSenderEpoch = op.MaxGiftsPerSenderEpoch
	// Content conviction staking
	p.ContentConvictionHalfLifeEpochs = op.ContentConvictionHalfLifeEpochs
	p.MaxContentStakePerMember = op.MaxContentStakePerMember
	p.MaxAuthorBondPerContent = op.MaxAuthorBondPerContent
	p.AuthorBondSlashOnModeration = op.AuthorBondSlashOnModeration
	// Content challenge reward share
	p.ContentChallengeRewardShare = op.ContentChallengeRewardShare
	// Conviction propagation
	p.ConvictionPropagationRatio = op.ConvictionPropagationRatio
	// Tag anti-gaming
	p.MaxTagsPerInitiative = op.MaxTagsPerInitiative
	// Anti-gaming
	p.ReputationDecayRate = op.ReputationDecayRate
	p.MaxConvictionSharePerMember = op.MaxConvictionSharePerMember
	p.InvitationStakeBurnRate = op.InvitationStakeBurnRate
	p.MaxReputationGainPerEpoch = op.MaxReputationGainPerEpoch
	return p
}

// ExtractOperationalParams extracts the operational fields from Params into RepOperationalParams.
func (p Params) ExtractOperationalParams() RepOperationalParams {
	return RepOperationalParams{
		// Time
		EpochBlocks:          p.EpochBlocks,
		SeasonDurationEpochs: p.SeasonDurationEpochs,
		// DREAM economics
		StakingApy:         p.StakingApy,
		UnstakedDecayRate:  p.UnstakedDecayRate,
		TransferTaxRate:    p.TransferTaxRate,
		MaxTipAmount:       p.MaxTipAmount,
		MaxTipsPerEpoch:    p.MaxTipsPerEpoch,
		MaxGiftAmount:      p.MaxGiftAmount,
		GiftOnlyToInvitees: p.GiftOnlyToInvitees,
		// Reputation
		MinReputationMultiplier: p.MinReputationMultiplier,
		// Review periods
		DefaultReviewPeriodEpochs:    p.DefaultReviewPeriodEpochs,
		DefaultChallengePeriodEpochs: p.DefaultChallengePeriodEpochs,
		// Invitations
		MinInvitationStake:             p.MinInvitationStake,
		InvitationAccountabilityEpochs: p.InvitationAccountabilityEpochs,
		ReferralRewardRate:             p.ReferralRewardRate,
		InvitationCostMultiplier:       p.InvitationCostMultiplier,
		// Challenges
		MinChallengeStake:    p.MinChallengeStake,
		ChallengerRewardRate: p.ChallengerRewardRate,
		JurySize:             p.JurySize,
		JurySuperMajority:    p.JurySuperMajority,
		MinJurorReputation:   p.MinJurorReputation,
		// Interim compensation
		SimpleComplexityBudget:   p.SimpleComplexityBudget,
		StandardComplexityBudget: p.StandardComplexityBudget,
		ComplexComplexityBudget:  p.ComplexComplexityBudget,
		ExpertComplexityBudget:   p.ExpertComplexityBudget,
		SoloExpertBonusRate:      p.SoloExpertBonusRate,
		InterimDeadlineEpochs:    p.InterimDeadlineEpochs,
		// Rate limits
		MaxActiveChallengesPerCommittee: p.MaxActiveChallengesPerCommittee,
		MaxNewChallengesPerEpoch:        p.MaxNewChallengesPerEpoch,
		ChallengeQueueMaxSize:           p.ChallengeQueueMaxSize,
		// Extended staking
		ProjectStakingApy:          p.ProjectStakingApy,
		ProjectCompletionBonusRate: p.ProjectCompletionBonusRate,
		MemberStakeRevenueShare:    p.MemberStakeRevenueShare,
		TagStakeRevenueShare:       p.TagStakeRevenueShare,
		MinStakeDurationSeconds:    p.MinStakeDurationSeconds,
		AllowSelfMemberStake:       p.AllowSelfMemberStake,
		// Challenge response deadline
		ChallengeResponseDeadlineEpochs: p.ChallengeResponseDeadlineEpochs,
		// Gift rate limiting
		GiftCooldownBlocks:     p.GiftCooldownBlocks,
		MaxGiftsPerSenderEpoch: p.MaxGiftsPerSenderEpoch,
		// Content conviction staking
		ContentConvictionHalfLifeEpochs: p.ContentConvictionHalfLifeEpochs,
		MaxContentStakePerMember:        p.MaxContentStakePerMember,
		MaxAuthorBondPerContent:         p.MaxAuthorBondPerContent,
		AuthorBondSlashOnModeration:     p.AuthorBondSlashOnModeration,
		// Content challenge reward share
		ContentChallengeRewardShare: p.ContentChallengeRewardShare,
		// Conviction propagation
		ConvictionPropagationRatio: p.ConvictionPropagationRatio,
		// Tag anti-gaming
		MaxTagsPerInitiative: p.MaxTagsPerInitiative,
		// Anti-gaming
		ReputationDecayRate:         p.ReputationDecayRate,
		MaxConvictionSharePerMember: p.MaxConvictionSharePerMember,
		InvitationStakeBurnRate:     p.InvitationStakeBurnRate,
		MaxReputationGainPerEpoch:   p.MaxReputationGainPerEpoch,
	}
}
