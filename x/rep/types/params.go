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
		MaxGiftAmount:      math.NewInt(500000000),           // 500 DREAM (500 * 1e6 micro-DREAM)
		GiftOnlyToInvitees: true,

		// Initiative rewards
		CompleterShare:          math.LegacyNewDecWithPrec(90, 2), // 90%
		TreasuryShare:           math.LegacyNewDecWithPrec(10, 2), // 10%
		MinReputationMultiplier: math.LegacyNewDecWithPrec(10, 2), // 10%

		// Initiative tiers (MaxBudget in micro-DREAM: 1 DREAM = 1,000,000 micro-DREAM)
		ApprenticeTier: TierConfig{
			MaxBudget:        math.NewInt(100000000),   // 100 DREAM
			MinReputation:    math.LegacyZeroDec(),
			ReputationCap:    math.LegacyNewDec(25),
			RewardMultiplier: math.LegacyNewDecWithPrec(50, 2), // 0.5x
		},
		StandardTier: TierConfig{
			MaxBudget:        math.NewInt(500000000),   // 500 DREAM
			MinReputation:    math.LegacyNewDec(25),
			ReputationCap:    math.LegacyNewDec(100),
			RewardMultiplier: math.LegacyOneDec(), // 1.0x
		},
		ExpertTier: TierConfig{
			MaxBudget:        math.NewInt(2000000000),  // 2000 DREAM
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
		ConvictionHalfLifeEpochs: 7,                                 // 7 epochs = 7 days half-life
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
		MinChallengeStake:      math.NewInt(50),
		AnonymousFeeMultiplier: math.LegacyNewDecWithPrec(250, 2), // 2.5x
		ChallengerRewardRate:   math.LegacyNewDecWithPrec(20, 2),  // 20%
		JurySize:               5,
		JurySuperMajority:      math.LegacyNewDecWithPrec(67, 2), // 67%
		MinJurorReputation:     math.LegacyNewDec(50),

		// Interim compensation - PRODUCTION values (in micro-DREAM: 1 DREAM = 1e6 micro-DREAM)
		SimpleComplexityBudget:   math.NewInt(50000000),   // 50 DREAM
		StandardComplexityBudget: math.NewInt(150000000),  // 150 DREAM
		ComplexComplexityBudget:  math.NewInt(400000000),  // 400 DREAM
		ExpertComplexityBudget:   math.NewInt(1000000000), // 1000 DREAM
		SoloExpertBonusRate:      math.LegacyNewDecWithPrec(50, 2), // 50%
		InterimDeadlineEpochs:    7,                                 // 7 epochs = ~1 week

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

	return nil
}
