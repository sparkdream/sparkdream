package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// RoleRewardPoolCeiling bounds every bonded-role SPARK reward pool cap and the
// daily community-pool draw.
//
// 1e18 uspark is a trillion SPARK — four orders of magnitude above any supply
// this chain can reach, so it never binds a real configuration. It exists
// because these caps are committee-editable and feed a multiplication in
// FundRoleRewardPools: math.Int panics past 256 bits, and a panic in BeginBlock
// halts the chain. Without a ceiling, a single mistyped operational-params
// proposal (an extra run of zeros) is a chain-halt bug, not a bad setting.
func RoleRewardPoolCeiling() math.Int {
	return math.NewInt(1_000_000_000_000_000_000)
}

// DefaultParams returns a default set of parameters.
// PRODUCTION values - use config.yml to override for testing/development.
func DefaultParams() Params {
	return Params{
		// Time - PRODUCTION values
		EpochBlocks:          14400, // ~1 day (14400 blocks * 6s = 86400s = 1 day)
		SeasonDurationEpochs: 150,   // ~5 months (150 days)

		// DREAM economics
		UnstakedDecayRate:         math.LegacyNewDecWithPrec(2, 3), // 0.2% per epoch (~73% annualized)
		StakedDecayRate:           math.LegacyNewDecWithPrec(5, 4), // 0.05% per epoch (~18% annualized)
		NewMemberDecayGraceEpochs: 30,                              // ~1 month grace period (no decay)
		TransferTaxRate:           math.LegacyNewDecWithPrec(3, 2), // 3%
		MaxTipAmount:              math.NewInt(100000000),          // 100 DREAM (100 * 1e6 micro-DREAM)
		MaxTipsPerEpoch:           10,
		MaxGiftAmount:             math.NewInt(500000000), // 500 DREAM (500 * 1e6 micro-DREAM)
		GiftOnlyToInvitees:        true,

		// Seasonal staking reward pool (replaces fixed StakingApy)
		MaxStakingRewardsPerSeason: math.NewInt(25000000000000), // 25,000 DREAM per season

		// Treasury management
		MaxTreasuryBalance:    math.NewInt(100000000000000), // 100,000 DREAM — excess burned
		TreasuryFundsInterims: true,                         // interims paid from treasury first
		TreasuryFundsRetroPgf: true,                         // retro PGF paid from treasury first

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

		// Invitations - PRODUCTION values (in micro-DREAM: 1 DREAM = 1e6 micro-DREAM)
		MinInvitationStake:             math.NewInt(100000000),          // 100 DREAM
		InvitationAccountabilityEpochs: 150,                             // 150 epochs = ~5 months (1 season)
		ReferralRewardRate:             math.LegacyNewDecWithPrec(5, 2), // 5%
		InvitationCostMultiplier:       getInvitationCostMultiplier(),   // 1.1x prod, 1.0 in testparams (see params_vals_*.go)

		// Trust levels configuration
		// NOTE: TrustLevelConfig values are hardcoded here because Ignite's YAML parser
		// cannot handle nested proto message structures in config.yml. To switch between
		// production and testing values, comment/uncomment the appropriate section.
		// See x/commons/keeper/genesis_vals.go for the same pattern.
		TrustLevelConfig: getTrustLevelConfig(),

		// Challenges (stake in micro-DREAM: 1 DREAM = 1e6 micro-DREAM)
		MinChallengeStake:    math.NewInt(50000000),            // 50 DREAM
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
		// ProjectStakingApy removed — projects draw from MaxStakingRewardsPerSeason pool
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
		// Majority of the stake behind an initiative must vote against it
		// before submitted work is abandoned.
		MaxReputationGainPerEpoch: math.LegacyNewDec(50), // Max 50 reputation per tag per epoch (prevents interim grinding)

		// Anti-whale staking cap (prevents reward pool extraction via disproportionate initiative stakes)
		MaxInitiativeStakePerMember: math.NewInt(50000000000), // 50,000 DREAM per member per initiative/project

		// Anti-collusion: per-season cap on total DREAM minted via initiative completion
		MaxInitiativeRewardsPerSeason: math.NewInt(100000000000000), // 100,000 DREAM per season

		// Anti-collusion: projects above this budget require council proposal approval (not single committee member)
		LargeProjectBudgetThreshold: math.NewInt(10000000000), // 10,000 DREAM (Epic tier max)

		// Permissionless creation fees (burned on creation — anti-spam + deflationary)
		ProjectCreationFee:              math.NewInt(5000000), // 5 DREAM
		InitiativeCreationFeeApprentice: math.NewInt(1000000), // 1 DREAM
		InitiativeCreationFeeStandard:   math.NewInt(3000000), // 3 DREAM
		TagCreationFee:                  math.NewInt(100),     // 100 micro-DREAM

		// Permissionless access control (governance-only)
		PermissionlessMinTrustLevel: 2, // ESTABLISHED
		PermissionlessMaxTier:       1, // STANDARD (0=APPRENTICE, 1=STANDARD)

		// Sentinel SPARK reward pool (Stage A infrastructure; funding + distribution added in later stages)
		MaxSentinelRewardPool:               math.NewInt(100000000000),        // 100,000 SPARK in uspark
		SentinelRewardPoolOverflowBurnRatio: math.LegacyNewDecWithPrec(5, 1),  // 0.5 (50%)
		SentinelRewardEpochBlocks:           getSentinelRewardEpochBlocks(),   // build-tag dependent (14400 production, 20 testparams)
		MinSentinelAccuracy:                 math.LegacyNewDecWithPrec(70, 2), // 0.70
		MinAppealsForAccuracy:               10,
		MinEpochActivityForReward:           1,
		MinAppealRate:                       math.LegacyNewDecWithPrec(5, 2), // 0.05
		SentinelAccuracyWindowEpochs:        DefaultSentinelAccuracyWindowEpochs,
		// Reviewer SPARK pool: same shape as the sentinel pool above, tuned
		// separately because the liability differs by orders of magnitude.
		MaxReviewerRewardPool:               math.NewInt(150000000000),        // 150,000 SPARK — 1.5x the sentinel/curator pools
		ReviewerRewardPoolOverflowBurnRatio: math.LegacyNewDecWithPrec(5, 1),  // 0.5 (50%)
		ReviewerRewardEpochBlocks:           getSentinelRewardEpochBlocks(),   // same build-tag cadence
		MinReviewerAccuracy:                 math.LegacyNewDecWithPrec(70, 2), // 0.70
		ReviewerAccuracyWindowEpochs:        DefaultSentinelAccuracyWindowEpochs,
		// Reviewer bonded-role policy, written through to the BondedRoleConfig for
		// ROLE_TYPE_INITIATIVE_REVIEWER on InitGenesis and on every operational
		// param update.
		//
		// 500 DREAM, deliberately a low barrier to entry. The floor is not what a
		// bad verdict costs: SlashReviewersOnOverturn charges the per-verdict
		// reserve (ReviewerBondReserveRate of the initiative's budget), so exposure
		// already scales with what the review could mint whatever the floor is. The
		// floor's job is to keep the role from being free to enter and to give
		// demotion something to bite on.
		//
		// Capacity is a separate decision the reviewer makes by bonding more.
		// Free bond above their open reserves is what gates which work they can
		// pick up and how much at once: at the default 10% rate the floor covers
		// budgets up to ~5,000 DREAM, while an EPIC initiative (10,000 cap)
		// reserves 1,000. Topping up is another MsgBondRole against the same
		// record -- it raises current_bond and is reservable immediately. Starting
		// small and growing into the role is the intended path.
		MinReviewerBond:           math.NewInt(500_000_000), // 500 DREAM
		ReviewerDemotionThreshold: math.NewInt(250_000_000), // 250 DREAM: half the floor
		MinReviewerTrustLevel:     "TRUST_LEVEL_ESTABLISHED",
		MinReviewerRepTier:        0, // trust level is the whole gate; see BondedRoleConfig notes
		MinReviewerAgeBlocks:      0,
		ReviewerDemotionCooldown:  604800,  // 7 days
		ReviewerUnbondCooldown:    1209600, // 14 days: bond stays slashable while open verdicts age out
		// One capped claim on the community pool per UTC day, divided across
		// every bonded-role pool by headroom. Expressed as a share of the
		// pool's inflation income so it scales with supply and takes less when
		// the pool is thin, instead of taking most of a poor pool and half of a
		// rich one.
		RoleRewardInflationShare: math.LegacyNewDecWithPrec(5, 1), // 0.5
		// Curator SPARK pool: sized equal to the sentinel pool, same cadence
		// and accuracy bar. Separate params so the two can diverge later
		// without one role's tuning silently moving the other's.
		MaxCuratorRewardPool:               math.NewInt(100000000000),        // 100,000 SPARK in uspark
		CuratorRewardPoolOverflowBurnRatio: math.LegacyNewDecWithPrec(5, 1),  // 0.5 (50%)
		CuratorRewardEpochBlocks:           getSentinelRewardEpochBlocks(),   // same build-tag cadence
		MinCuratorAccuracy:                 math.LegacyNewDecWithPrec(70, 2), // 0.70
		CuratorAccuracyWindowEpochs:        DefaultSentinelAccuracyWindowEpochs,
		// Federation-verifier pay. Pool sized equal to the sentinel and curator
		// pools: an idle role draws nothing under headroom-proportional funding,
		// so an equal cap costs the community pool nothing while the roster is
		// small and avoids inventing a per-role sizing story with no evidence
		// behind it yet. Its own cadence (see getVerifierRewardEpochBlocks) and
		// its own accuracy bar, matching federation's pre-migration 0.8.
		MaxVerifierRewardPool:               math.NewInt(100000000000),       // 100,000 SPARK in uspark
		VerifierRewardPoolOverflowBurnRatio: math.LegacyNewDecWithPrec(5, 1), // 0.5 (50%)
		VerifierRewardEpochBlocks:           getVerifierRewardEpochBlocks(),
		MinVerifierAccuracy:                 getMinVerifierAccuracy(),
		VerifierAccuracyWindowEpochs:        DefaultSentinelAccuracyWindowEpochs,
		MinEpochVerifications:               getMinEpochVerifications(),
		VerifierDreamReward:                 math.NewInt(5000000), // 5 DREAM
		MaxVerifierDreamMintPerEpoch:        getMaxVerifierDreamMintPerEpoch(),
		// Chain-wide review gate, keyed on how much the completion mints.
		// 100 DREAM is the APPRENTICE ceiling, so apprentice work stays exempt
		// and every permissionless STANDARD initiative is gated.
		ReviewRequiredAboveBudget: math.NewInt(100000000), // 100 DREAM
		// A bounty must sit a full epoch before it can be pulled, so
		// advertising one and withdrawing it is not free.
		ReviewBountyReclaimDelay: 14400, // ~1 day
		// Permissionless work pays for the review its own minting consumes,
		// in existing DREAM rather than by diluting everyone.
		PermissionlessMinReviewBountyRate: math.LegacyNewDecWithPrec(1, 1), // 0.1

		// Per-member active work caps (anti-monopolization)
		MaxActiveInitiativesPerMember: 10,
		MaxActiveInterimsPerMember:    10,

		// Global per-epoch DREAM minting ceiling (anti-inflation safety net).
		// 10,000 DREAM per epoch; at 150 epochs/season this bounds total inflation
		// to ~1.5M DREAM/season even under pathological rubber-stamping.
		MaxDreamMintPerEpoch: math.NewInt(10000000000000),

		// Proposal-time hard caps. ~100× the routing threshold (10K DREAM) and
		// 100K SPARK — never bites a legitimate proposal but rejects nonsense
		// values that would pollute state.
		MaxProjectRequestedBudget: math.NewInt(1000000000000), // 1,000,000 DREAM (micro-DREAM)
		MaxProjectRequestedSpark:  math.NewInt(100000000000),  // 100,000 SPARK (uspark)

		// PROPOSED projects expire ~2 weeks after creation at 6s blocks
		// (200,000 blocks ≈ 13.9 days). Tunable; tests can shorten via the
		// operational params if a faster e2e cycle is needed.
		ProposedProjectExpiryBlocks: 200000,

		// Self-assignment safeguards (self-assigned initiatives)
		SelfAssignedBondRate:                math.LegacyNewDecWithPrec(10, 2), // 10% of budget
		SelfAssignedExternalConvictionRatio: math.LegacyOneDec(),              // 100% external
		SelfAssignedChallengeMultiplier:     2,
		// Permissionless self-assignment mints DREAM nobody approved rather
		// than moving DREAM governance already allocated, so it carries a
		// heavier bond. At the STANDARD tier ceiling (500 DREAM) this is 125
		// DREAM locked to self-assign — returned on completion, burned if a
		// challenge is upheld.
		PermissionlessSelfAssignedBondRate: math.LegacyNewDecWithPrec(25, 2), // 25% of the mint

		// Reputation charged per tag for accepting a summons and abandoning it.
		// At MinJurorReputation 50, four abandoned seats cost an otherwise
		// qualified juror their eligibility in that tag.
		AbandonedJurySeatPenalty: math.LegacyNewDec(10),
		// A quarter of the disputed budget pays the jury, split across the
		// seats. Settling a dispute should cost a fraction of what is in
		// dispute, not several times it.
		JurorRewardRate: math.LegacyNewDecWithPrec(25, 2), // 25%
		// Params-only, like the self-assign bond rates: this governs how long a
		// conscripted juror has to answer before losing the seat, so governance
		// moves it and the Operations Committee does not.
		JuryAcceptanceWindowRatio:     math.LegacyNewDecWithPrec(25, 2), // 25% of the review period
		MinJurorReward:                math.NewInt(5_000_000),           // 5 DREAM
		MinJurorSelectionWeight:       math.LegacyNewDecWithPrec(1, 1),  // 0.1
		MinJurySeatingsForWeighting:   3,
		InitiativeCompletionBonusRate: math.LegacyNewDecWithPrec(1, 1), // 10% of budget
		MaxJuryRedraws:                1,
		ReviewerBondReserveRate:       math.LegacyNewDecWithPrec(1, 1), // 10% of budget per verdict
		ReviewFeeRate:                 math.LegacyNewDecWithPrec(5, 2), // 5% of budget to reviewers
		MaxReviewRounds:               3,
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
		return fmt.Errorf("unstaked decay rate cannot be negative: %s", p.UnstakedDecayRate)
	}
	if p.UnstakedDecayRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("unstaked decay rate cannot be greater than 1: %s", p.UnstakedDecayRate)
	}
	if p.StakedDecayRate.IsNegative() {
		return fmt.Errorf("staked decay rate cannot be negative: %s", p.StakedDecayRate)
	}
	if p.StakedDecayRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("staked decay rate cannot be greater than 1: %s", p.StakedDecayRate)
	}
	if p.NewMemberDecayGraceEpochs < 0 {
		return fmt.Errorf("new member decay grace epochs cannot be negative: %d", p.NewMemberDecayGraceEpochs)
	}
	if p.MaxStakingRewardsPerSeason.IsNegative() {
		return fmt.Errorf("max staking rewards per season cannot be negative: %s", p.MaxStakingRewardsPerSeason)
	}
	if p.MaxTreasuryBalance.IsNegative() {
		return fmt.Errorf("max treasury balance cannot be negative: %s", p.MaxTreasuryBalance)
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
	// ...and must leave the redraw sweep somewhere to go. `vacatable` is
	// len(jurors) - MinSeatedJurors, so at jury_size == MinSeatedJurors it is
	// always zero and the sweep silently vacates nothing, replaces nothing and
	// returns — the acceptance window, redraws and replacement selection all
	// become dead code. Below the floor a jury can never reach quorum at all
	// (TallyJuryVotes floors it), so every challenge would escalate. Oddness
	// alone let both through: 1 and 3 are both odd.
	if p.JurySize <= MinSeatedJurors {
		return fmt.Errorf("jury size must exceed the seated-jury floor %d, got %d",
			MinSeatedJurors, p.JurySize)
	}
	if p.ReviewerBondReserveRate.IsNil() || !p.ReviewerBondReserveRate.IsPositive() ||
		p.ReviewerBondReserveRate.GT(math.LegacyOneDec()) {
		// Zero would let a reviewer approve a mint with nothing at risk, which
		// is the entire accountability of the role.
		return fmt.Errorf("reviewer bond reserve rate must be in (0,1]: %s", p.ReviewerBondReserveRate)
	}
	if err := p.ReviewerBondPolicy().Validate(); err != nil {
		return err
	}
	if p.ReviewFeeRate.IsNil() || p.ReviewFeeRate.IsNegative() ||
		p.ReviewFeeRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("review fee rate must be in [0,1]: %s", p.ReviewFeeRate)
	}
	if p.MaxReviewerRewardPool.IsNil() || p.MaxReviewerRewardPool.IsNegative() {
		return fmt.Errorf("max reviewer reward pool must be non-negative: %s", p.MaxReviewerRewardPool)
	}
	if p.MaxReviewerRewardPool.GT(RoleRewardPoolCeiling()) {
		return fmt.Errorf("max reviewer reward pool exceeds ceiling %s: %s", RoleRewardPoolCeiling(), p.MaxReviewerRewardPool)
	}
	if p.ReviewerRewardPoolOverflowBurnRatio.IsNil() || p.ReviewerRewardPoolOverflowBurnRatio.IsNegative() ||
		p.ReviewerRewardPoolOverflowBurnRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("reviewer reward pool overflow burn ratio must be in [0,1]: %s", p.ReviewerRewardPoolOverflowBurnRatio)
	}
	if p.ReviewerRewardEpochBlocks == 0 {
		// Zero would divide by zero when deriving the epoch number.
		return fmt.Errorf("reviewer reward epoch blocks must be positive")
	}
	// Zero is meaningful here: it disables automatic funding entirely and
	// returns the pools to manual top-ups. The upper bound is 1 — a share
	// above the community pool's whole inflation income would mean x/rep
	// intends to leave the councils nothing, which should not be expressible.
	if p.RoleRewardInflationShare.IsNil() || p.RoleRewardInflationShare.IsNegative() ||
		p.RoleRewardInflationShare.GT(math.LegacyOneDec()) {
		return fmt.Errorf("role reward inflation share must be in [0,1]: %s", p.RoleRewardInflationShare)
	}
	if p.MaxCuratorRewardPool.IsNil() || p.MaxCuratorRewardPool.IsNegative() {
		return fmt.Errorf("max curator reward pool must be non-negative: %s", p.MaxCuratorRewardPool)
	}
	if p.MaxCuratorRewardPool.GT(RoleRewardPoolCeiling()) {
		return fmt.Errorf("max curator reward pool exceeds ceiling %s: %s", RoleRewardPoolCeiling(), p.MaxCuratorRewardPool)
	}
	if p.CuratorRewardPoolOverflowBurnRatio.IsNil() || p.CuratorRewardPoolOverflowBurnRatio.IsNegative() ||
		p.CuratorRewardPoolOverflowBurnRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("curator reward pool overflow burn ratio must be in [0,1]: %s", p.CuratorRewardPoolOverflowBurnRatio)
	}
	if p.CuratorRewardEpochBlocks == 0 {
		// Zero would divide by zero when deriving the epoch number.
		return fmt.Errorf("curator reward epoch blocks must be positive")
	}
	if p.MinCuratorAccuracy.IsNil() || p.MinCuratorAccuracy.IsNegative() ||
		p.MinCuratorAccuracy.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min curator accuracy must be in [0,1]: %s", p.MinCuratorAccuracy)
	}
	if p.CuratorAccuracyWindowEpochs == 0 {
		return fmt.Errorf("curator accuracy window epochs must be positive")
	}
	if p.MaxVerifierRewardPool.IsNil() || p.MaxVerifierRewardPool.IsNegative() {
		return fmt.Errorf("max verifier reward pool must be non-negative: %s", p.MaxVerifierRewardPool)
	}
	if p.MaxVerifierRewardPool.GT(RoleRewardPoolCeiling()) {
		return fmt.Errorf("max verifier reward pool exceeds ceiling %s: %s", RoleRewardPoolCeiling(), p.MaxVerifierRewardPool)
	}
	if p.VerifierRewardPoolOverflowBurnRatio.IsNil() || p.VerifierRewardPoolOverflowBurnRatio.IsNegative() ||
		p.VerifierRewardPoolOverflowBurnRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("verifier reward pool overflow burn ratio must be in [0,1]: %s", p.VerifierRewardPoolOverflowBurnRatio)
	}
	if p.VerifierRewardEpochBlocks == 0 {
		// Zero would divide by zero when deriving the epoch number.
		return fmt.Errorf("verifier reward epoch blocks must be positive")
	}
	if p.MinVerifierAccuracy.IsNil() || p.MinVerifierAccuracy.IsNegative() ||
		p.MinVerifierAccuracy.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min verifier accuracy must be in [0,1]: %s", p.MinVerifierAccuracy)
	}
	if p.VerifierAccuracyWindowEpochs == 0 {
		return fmt.Errorf("verifier accuracy window epochs must be positive")
	}
	if p.VerifierDreamReward.IsNil() || p.VerifierDreamReward.IsNegative() {
		return fmt.Errorf("verifier dream reward must be non-negative: %s", p.VerifierDreamReward)
	}
	if p.MaxVerifierDreamMintPerEpoch.IsNil() || p.MaxVerifierDreamMintPerEpoch.IsNegative() {
		return fmt.Errorf("max verifier dream mint per epoch must be non-negative: %s", p.MaxVerifierDreamMintPerEpoch)
	}
	// Zero is meaningful: it disables the chain-wide gate and leaves review to
	// per-project policy.
	if p.ReviewRequiredAboveBudget.IsNil() || p.ReviewRequiredAboveBudget.IsNegative() {
		return fmt.Errorf("review required above budget must be non-negative: %s", p.ReviewRequiredAboveBudget)
	}
	if p.PermissionlessMinReviewBountyRate.IsNil() || p.PermissionlessMinReviewBountyRate.IsNegative() ||
		p.PermissionlessMinReviewBountyRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("permissionless min review bounty rate must be in [0,1]: %s", p.PermissionlessMinReviewBountyRate)
	}
	if p.MinReviewerAccuracy.IsNil() || p.MinReviewerAccuracy.IsNegative() ||
		p.MinReviewerAccuracy.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min reviewer accuracy must be in [0,1]: %s", p.MinReviewerAccuracy)
	}
	if p.ReviewerAccuracyWindowEpochs == 0 || p.ReviewerAccuracyWindowEpochs > MaxSentinelAccuracyWindowEpochs {
		return fmt.Errorf("reviewer accuracy window epochs must be in [1,%d]: %d",
			MaxSentinelAccuracyWindowEpochs, p.ReviewerAccuracyWindowEpochs)
	}
	if p.MaxReviewRounds == 0 {
		// Zero rounds means a rejection can never be remedied and the work is
		// stuck; one round is the minimum that still allows a resubmission.
		return fmt.Errorf("max review rounds must be at least 1")
	}
	if p.MinJurorReward.IsNil() || p.MinJurorReward.IsNegative() {
		return fmt.Errorf("min juror reward must be non-negative: %s", p.MinJurorReward)
	}
	if p.MinJurorSelectionWeight.IsNil() || !p.MinJurorSelectionWeight.IsPositive() ||
		p.MinJurorSelectionWeight.GT(math.LegacyOneDec()) {
		// Zero would exclude a non-responder in all but name: an address drawn
		// with zero weight can never be drawn again, so it could never earn its
		// standing back.
		return fmt.Errorf("min juror selection weight must be in (0,1]: %s", p.MinJurorSelectionWeight)
	}
	if p.InitiativeCompletionBonusRate.IsNil() || p.InitiativeCompletionBonusRate.IsNegative() ||
		p.InitiativeCompletionBonusRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("initiative completion bonus rate must be in [0,1]: %s",
			p.InitiativeCompletionBonusRate)
	}
	// Each redraw round costs one acceptance window out of the review period,
	// so the rounds and the window have to fit inside it together.
	if !p.JuryAcceptanceWindowRatio.IsNil() &&
		p.JuryAcceptanceWindowRatio.MulInt64(int64(p.MaxJuryRedraws+1)).GTE(math.LegacyOneDec()) {
		return fmt.Errorf(
			"jury acceptance window ratio %s x %d redraw rounds consumes the whole review period",
			p.JuryAcceptanceWindowRatio, p.MaxJuryRedraws+1)
	}

	// Jury super-majority must be in (0,1]; >1 deadlocks every jury/appeal.
	if p.JurySuperMajority.IsNil() || !p.JurySuperMajority.IsPositive() || p.JurySuperMajority.GT(math.LegacyOneDec()) {
		return fmt.Errorf("jury super majority must be in (0,1]: %s", p.JurySuperMajority)
	}

	// Transfer tax rate must be in [0,1].
	if p.TransferTaxRate.IsNegative() {
		return fmt.Errorf("transfer tax rate cannot be negative: %s", p.TransferTaxRate)
	}
	if p.TransferTaxRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("transfer tax rate cannot be greater than 1: %s", p.TransferTaxRate)
	}

	// Conviction parameters.
	if p.ExternalConvictionRatio.IsNegative() || p.ExternalConvictionRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("external conviction ratio must be in [0,1]: %s", p.ExternalConvictionRatio)
	}

	// Self-assignment safeguards.
	if p.SelfAssignedBondRate.IsNil() || p.SelfAssignedBondRate.IsNegative() || p.SelfAssignedBondRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("self-assigned bond rate must be in [0,1]: %s", p.SelfAssignedBondRate)
	}
	if p.AbandonedJurySeatPenalty.IsNil() || p.AbandonedJurySeatPenalty.IsNegative() {
		return fmt.Errorf("abandoned jury seat penalty cannot be negative: %s", p.AbandonedJurySeatPenalty)
	}
	if p.JuryAcceptanceWindowRatio.IsNil() || !p.JuryAcceptanceWindowRatio.IsPositive() ||
		p.JuryAcceptanceWindowRatio.GTE(math.LegacyOneDec()) {
		// Zero would sweep every seat on the block it was drawn; 1 or more puts
		// the acceptance deadline at or past the vote deadline, which is the
		// state that made the sweep unreachable on short-period networks.
		return fmt.Errorf("jury acceptance window ratio must be in (0,1): %s", p.JuryAcceptanceWindowRatio)
	}
	if p.JurorRewardRate.IsNil() || p.JurorRewardRate.IsNegative() ||
		p.JurorRewardRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("juror reward rate must be in [0,1]: %s", p.JurorRewardRate)
	}
	if p.PermissionlessSelfAssignedBondRate.IsNil() ||
		p.PermissionlessSelfAssignedBondRate.IsNegative() ||
		p.PermissionlessSelfAssignedBondRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("permissionless self-assigned bond rate must be in [0,1]: %s", p.PermissionlessSelfAssignedBondRate)
	}
	if p.SelfAssignedExternalConvictionRatio.IsNil() ||
		p.SelfAssignedExternalConvictionRatio.LT(p.ExternalConvictionRatio) ||
		p.SelfAssignedExternalConvictionRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("self-assigned external conviction ratio must be in [external_conviction_ratio, 1]: %s", p.SelfAssignedExternalConvictionRatio)
	}
	if p.SelfAssignedChallengeMultiplier < 1 {
		return fmt.Errorf("self-assigned challenge multiplier must be >= 1: %d", p.SelfAssignedChallengeMultiplier)
	}
	if p.ConvictionPerDream.IsNil() || !p.ConvictionPerDream.IsPositive() {
		return fmt.Errorf("conviction per dream must be positive: %s", p.ConvictionPerDream)
	}
	if p.ConvictionHalfLifeEpochs <= 0 {
		return fmt.Errorf("conviction half life epochs must be positive: %d", p.ConvictionHalfLifeEpochs)
	}

	// Review/challenge windows must be positive; 0 yields an instant deadline.
	if p.DefaultReviewPeriodEpochs <= 0 {
		return fmt.Errorf("default review period epochs must be positive: %d", p.DefaultReviewPeriodEpochs)
	}
	if p.DefaultChallengePeriodEpochs <= 0 {
		return fmt.Errorf("default challenge period epochs must be positive: %d", p.DefaultChallengePeriodEpochs)
	}

	// Slash penalties must each be in [0,1]; >1 over-slashes.
	if p.MinorSlashPenalty.IsNegative() || p.MinorSlashPenalty.GT(math.LegacyOneDec()) {
		return fmt.Errorf("minor slash penalty must be in [0,1]: %s", p.MinorSlashPenalty)
	}
	if p.ModerateSlashPenalty.IsNegative() || p.ModerateSlashPenalty.GT(math.LegacyOneDec()) {
		return fmt.Errorf("moderate slash penalty must be in [0,1]: %s", p.ModerateSlashPenalty)
	}
	if p.SevereSlashPenalty.IsNegative() || p.SevereSlashPenalty.GT(math.LegacyOneDec()) {
		return fmt.Errorf("severe slash penalty must be in [0,1]: %s", p.SevereSlashPenalty)
	}
	if p.ZeroingSlashPenalty.IsNegative() || p.ZeroingSlashPenalty.GT(math.LegacyOneDec()) {
		return fmt.Errorf("zeroing slash penalty must be in [0,1]: %s", p.ZeroingSlashPenalty)
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

	// Anti-whale staking cap
	if !p.MaxInitiativeStakePerMember.IsPositive() {
		return fmt.Errorf("max initiative stake per member must be positive: %s", p.MaxInitiativeStakePerMember)
	}

	// Anti-collusion caps
	if !p.MaxInitiativeRewardsPerSeason.IsPositive() {
		return fmt.Errorf("max initiative rewards per season must be positive: %s", p.MaxInitiativeRewardsPerSeason)
	}
	if !p.LargeProjectBudgetThreshold.IsPositive() {
		return fmt.Errorf("large project budget threshold must be positive: %s", p.LargeProjectBudgetThreshold)
	}

	// Permissionless creation fees
	if p.ProjectCreationFee.IsNegative() {
		return fmt.Errorf("project creation fee cannot be negative: %s", p.ProjectCreationFee)
	}
	if p.InitiativeCreationFeeApprentice.IsNegative() {
		return fmt.Errorf("initiative creation fee (apprentice) cannot be negative: %s", p.InitiativeCreationFeeApprentice)
	}
	if p.InitiativeCreationFeeStandard.IsNegative() {
		return fmt.Errorf("initiative creation fee (standard) cannot be negative: %s", p.InitiativeCreationFeeStandard)
	}
	if p.TagCreationFee.IsNegative() {
		return fmt.Errorf("tag creation fee cannot be negative: %s", p.TagCreationFee)
	}
	if p.PermissionlessMinTrustLevel > 4 {
		return fmt.Errorf("permissionless min trust level must be 0-4: %d", p.PermissionlessMinTrustLevel)
	}
	if p.PermissionlessMaxTier > 3 {
		return fmt.Errorf("permissionless max tier must be 0-3: %d", p.PermissionlessMaxTier)
	}

	// Sentinel reward pool validation
	if p.MaxSentinelRewardPool.IsNegative() {
		return fmt.Errorf("max sentinel reward pool cannot be negative: %s", p.MaxSentinelRewardPool)
	}
	if !p.MaxSentinelRewardPool.IsNil() && p.MaxSentinelRewardPool.GT(RoleRewardPoolCeiling()) {
		return fmt.Errorf("max sentinel reward pool exceeds ceiling %s: %s", RoleRewardPoolCeiling(), p.MaxSentinelRewardPool)
	}
	if p.SentinelRewardPoolOverflowBurnRatio.IsNegative() {
		return fmt.Errorf("sentinel reward pool overflow burn ratio cannot be negative: %s", p.SentinelRewardPoolOverflowBurnRatio)
	}
	if p.SentinelRewardPoolOverflowBurnRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("sentinel reward pool overflow burn ratio cannot be greater than 1: %s", p.SentinelRewardPoolOverflowBurnRatio)
	}
	if p.SentinelRewardEpochBlocks == 0 {
		return fmt.Errorf("sentinel reward epoch blocks must be positive")
	}
	if p.MinSentinelAccuracy.IsNegative() || p.MinSentinelAccuracy.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min sentinel accuracy must be in [0,1]: %s", p.MinSentinelAccuracy)
	}
	if p.MinAppealRate.IsNegative() || p.MinAppealRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min appeal rate must be in [0,1]: %s", p.MinAppealRate)
	}
	if p.SentinelAccuracyWindowEpochs == 0 || p.SentinelAccuracyWindowEpochs > MaxSentinelAccuracyWindowEpochs {
		return fmt.Errorf("sentinel accuracy window epochs must be in [1,%d]: %d", MaxSentinelAccuracyWindowEpochs, p.SentinelAccuracyWindowEpochs)
	}

	// DREAM emission cap must be strictly positive; zero/negative/nil disallowed.
	if p.MaxDreamMintPerEpoch.IsNil() || p.MaxDreamMintPerEpoch.IsZero() || p.MaxDreamMintPerEpoch.IsNegative() {
		return fmt.Errorf("max_dream_mint_per_epoch must be positive")
	}

	// Proposal-time caps must be strictly positive — a zero cap would block all
	// proposals (including legitimate council-gated large ones), defeating the
	// design. Use a very high value to disable spam protection, not zero.
	if !p.MaxProjectRequestedBudget.IsPositive() {
		return fmt.Errorf("max project requested budget must be positive: %s", p.MaxProjectRequestedBudget)
	}
	if !p.MaxProjectRequestedSpark.IsPositive() {
		return fmt.Errorf("max project requested spark must be positive: %s", p.MaxProjectRequestedSpark)
	}
	if p.ProposedProjectExpiryBlocks <= 0 {
		return fmt.Errorf("proposed project expiry blocks must be positive: %d", p.ProposedProjectExpiryBlocks)
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
		UnstakedDecayRate:         math.LegacyNewDecWithPrec(2, 3), // 0.2%
		StakedDecayRate:           math.LegacyNewDecWithPrec(5, 4), // 0.05%
		NewMemberDecayGraceEpochs: 30,
		TransferTaxRate:           math.LegacyNewDecWithPrec(3, 2), // 3%
		MaxTipAmount:              math.NewInt(100000000),          // 100 DREAM
		MaxTipsPerEpoch:           10,
		MaxGiftAmount:             math.NewInt(500000000), // 500 DREAM
		GiftOnlyToInvitees:        true,
		// Seasonal staking reward pool
		MaxStakingRewardsPerSeason: math.NewInt(25000000000000), // 25,000 DREAM
		// Treasury management
		MaxTreasuryBalance:    math.NewInt(100000000000000), // 100,000 DREAM
		TreasuryFundsInterims: true,
		TreasuryFundsRetroPgf: true,
		// Reputation
		MinReputationMultiplier: math.LegacyNewDecWithPrec(10, 2), // 10%
		// Review periods
		DefaultReviewPeriodEpochs:    7,
		DefaultChallengePeriodEpochs: 7,
		// Invitations (stake in micro-DREAM)
		MinInvitationStake:             math.NewInt(100000000), // 100 DREAM
		InvitationAccountabilityEpochs: 150,
		ReferralRewardRate:             math.LegacyNewDecWithPrec(5, 2), // 5%
		InvitationCostMultiplier:       getInvitationCostMultiplier(),   // 1.1x prod, 1.0 in testparams
		// Challenges (stake in micro-DREAM)
		MinChallengeStake:    math.NewInt(50000000),            // 50 DREAM
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
		// Majority of staked DREAM required to abandon submitted work.
		AbandonedJurySeatPenalty:      math.LegacyNewDec(10),            // reputation, per tag
		JurorRewardRate:               math.LegacyNewDecWithPrec(25, 2), // 25% of the disputed budget
		MinJurorReward:                math.NewInt(5_000_000),           // 5 DREAM
		MinJurorSelectionWeight:       math.LegacyNewDecWithPrec(1, 1),  // 0.1
		MinJurySeatingsForWeighting:   3,
		InitiativeCompletionBonusRate: math.LegacyNewDecWithPrec(1, 1),  // 10% of budget
		JuryAcceptanceWindowRatio:     math.LegacyNewDecWithPrec(25, 2), // 25% of the review period
		MaxJuryRedraws:                1,
		ReviewerBondReserveRate:       math.LegacyNewDecWithPrec(1, 1),
		ReviewFeeRate:                 math.LegacyNewDecWithPrec(5, 2),
		MaxReviewRounds:               3,
		MaxReputationGainPerEpoch:     math.LegacyNewDec(50), // Max 50 per tag per epoch
		// Anti-whale staking cap
		MaxInitiativeStakePerMember: math.NewInt(50000000000), // 50,000 DREAM
		// Anti-collusion caps
		MaxInitiativeRewardsPerSeason: math.NewInt(100000000000000), // 100,000 DREAM
		LargeProjectBudgetThreshold:   math.NewInt(10000000000),     // 10,000 DREAM
		// Permissionless creation fees
		ProjectCreationFee:              math.NewInt(5000000), // 5 DREAM
		InitiativeCreationFeeApprentice: math.NewInt(1000000), // 1 DREAM
		InitiativeCreationFeeStandard:   math.NewInt(3000000), // 3 DREAM
		TagCreationFee:                  math.NewInt(100),     // 100 micro-DREAM

		// Sentinel SPARK reward pool
		MaxSentinelRewardPool:               math.NewInt(100000000000),        // 100,000 SPARK in uspark
		SentinelRewardPoolOverflowBurnRatio: math.LegacyNewDecWithPrec(5, 1),  // 0.5 (50%)
		SentinelRewardEpochBlocks:           getSentinelRewardEpochBlocks(),   // build-tag dependent (14400 production, 20 testparams)
		MinSentinelAccuracy:                 math.LegacyNewDecWithPrec(70, 2), // 0.70
		MinAppealsForAccuracy:               10,
		MinEpochActivityForReward:           1,
		MinAppealRate:                       math.LegacyNewDecWithPrec(5, 2), // 0.05
		SentinelAccuracyWindowEpochs:        DefaultSentinelAccuracyWindowEpochs,
		// Reviewer SPARK pool: same shape as the sentinel pool above, tuned
		// separately because the liability differs by orders of magnitude.
		MaxReviewerRewardPool:               math.NewInt(150000000000),        // 150,000 SPARK — 1.5x the sentinel/curator pools
		ReviewerRewardPoolOverflowBurnRatio: math.LegacyNewDecWithPrec(5, 1),  // 0.5 (50%)
		ReviewerRewardEpochBlocks:           getSentinelRewardEpochBlocks(),   // same build-tag cadence
		MinReviewerAccuracy:                 math.LegacyNewDecWithPrec(70, 2), // 0.70
		ReviewerAccuracyWindowEpochs:        DefaultSentinelAccuracyWindowEpochs,
		// Reviewer bonded-role policy, written through to the BondedRoleConfig for
		// ROLE_TYPE_INITIATIVE_REVIEWER on InitGenesis and on every operational
		// param update.
		//
		// 500 DREAM, deliberately a low barrier to entry. The floor is not what a
		// bad verdict costs: SlashReviewersOnOverturn charges the per-verdict
		// reserve (ReviewerBondReserveRate of the initiative's budget), so exposure
		// already scales with what the review could mint whatever the floor is. The
		// floor's job is to keep the role from being free to enter and to give
		// demotion something to bite on.
		//
		// Capacity is a separate decision the reviewer makes by bonding more.
		// Free bond above their open reserves is what gates which work they can
		// pick up and how much at once: at the default 10% rate the floor covers
		// budgets up to ~5,000 DREAM, while an EPIC initiative (10,000 cap)
		// reserves 1,000. Topping up is another MsgBondRole against the same
		// record -- it raises current_bond and is reservable immediately. Starting
		// small and growing into the role is the intended path.
		MinReviewerBond:           math.NewInt(500_000_000), // 500 DREAM
		ReviewerDemotionThreshold: math.NewInt(250_000_000), // 250 DREAM: half the floor
		MinReviewerTrustLevel:     "TRUST_LEVEL_ESTABLISHED",
		MinReviewerRepTier:        0, // trust level is the whole gate; see BondedRoleConfig notes
		MinReviewerAgeBlocks:      0,
		ReviewerDemotionCooldown:  604800,  // 7 days
		ReviewerUnbondCooldown:    1209600, // 14 days: bond stays slashable while open verdicts age out
		// One capped claim on the community pool per UTC day, divided across
		// every bonded-role pool by headroom. Expressed as a share of the
		// pool's inflation income so it scales with supply and takes less when
		// the pool is thin, instead of taking most of a poor pool and half of a
		// rich one.
		RoleRewardInflationShare: math.LegacyNewDecWithPrec(5, 1), // 0.5
		// Curator SPARK pool: sized equal to the sentinel pool, same cadence
		// and accuracy bar. Separate params so the two can diverge later
		// without one role's tuning silently moving the other's.
		MaxCuratorRewardPool:               math.NewInt(100000000000),        // 100,000 SPARK in uspark
		CuratorRewardPoolOverflowBurnRatio: math.LegacyNewDecWithPrec(5, 1),  // 0.5 (50%)
		CuratorRewardEpochBlocks:           getSentinelRewardEpochBlocks(),   // same build-tag cadence
		MinCuratorAccuracy:                 math.LegacyNewDecWithPrec(70, 2), // 0.70
		CuratorAccuracyWindowEpochs:        DefaultSentinelAccuracyWindowEpochs,
		// Federation-verifier pay. Pool sized equal to the sentinel and curator
		// pools: an idle role draws nothing under headroom-proportional funding,
		// so an equal cap costs the community pool nothing while the roster is
		// small and avoids inventing a per-role sizing story with no evidence
		// behind it yet. Its own cadence (see getVerifierRewardEpochBlocks) and
		// its own accuracy bar, matching federation's pre-migration 0.8.
		MaxVerifierRewardPool:               math.NewInt(100000000000),       // 100,000 SPARK in uspark
		VerifierRewardPoolOverflowBurnRatio: math.LegacyNewDecWithPrec(5, 1), // 0.5 (50%)
		VerifierRewardEpochBlocks:           getVerifierRewardEpochBlocks(),
		MinVerifierAccuracy:                 getMinVerifierAccuracy(),
		VerifierAccuracyWindowEpochs:        DefaultSentinelAccuracyWindowEpochs,
		MinEpochVerifications:               getMinEpochVerifications(),
		VerifierDreamReward:                 math.NewInt(5000000), // 5 DREAM
		MaxVerifierDreamMintPerEpoch:        getMaxVerifierDreamMintPerEpoch(),
		// Chain-wide review gate, keyed on how much the completion mints.
		// 100 DREAM is the APPRENTICE ceiling, so apprentice work stays exempt
		// and every permissionless STANDARD initiative is gated.
		ReviewRequiredAboveBudget: math.NewInt(100000000), // 100 DREAM
		// A bounty must sit a full epoch before it can be pulled, so
		// advertising one and withdrawing it is not free.
		ReviewBountyReclaimDelay: 14400, // ~1 day
		// Permissionless work pays for the review its own minting consumes,
		// in existing DREAM rather than by diluting everyone.
		PermissionlessMinReviewBountyRate: math.LegacyNewDecWithPrec(1, 1), // 0.1

		// Per-member active work caps
		MaxActiveInitiativesPerMember: 10,
		MaxActiveInterimsPerMember:    10,

		// Global per-epoch DREAM minting ceiling (10,000 DREAM/epoch)
		MaxDreamMintPerEpoch: math.NewInt(10000000000000),

		// Proposal-time hard caps (mirror Params.max_project_requested_*).
		MaxProjectRequestedBudget: math.NewInt(1000000000000), // 1,000,000 DREAM
		MaxProjectRequestedSpark:  math.NewInt(100000000000),  // 100,000 SPARK

		// PROPOSED-project expiry (mirror Params.proposed_project_expiry_blocks).
		ProposedProjectExpiryBlocks: 200000,
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
	if op.StakedDecayRate.IsNegative() {
		return fmt.Errorf("staked decay rate cannot be negative: %s", op.StakedDecayRate)
	}
	if op.StakedDecayRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("staked decay rate cannot be greater than 1: %s", op.StakedDecayRate)
	}
	if op.NewMemberDecayGraceEpochs < 0 {
		return fmt.Errorf("new member decay grace epochs cannot be negative: %d", op.NewMemberDecayGraceEpochs)
	}
	if op.MaxStakingRewardsPerSeason.IsNegative() {
		return fmt.Errorf("max staking rewards per season cannot be negative: %s", op.MaxStakingRewardsPerSeason)
	}
	if op.MaxTreasuryBalance.IsNegative() {
		return fmt.Errorf("max treasury balance cannot be negative: %s", op.MaxTreasuryBalance)
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
	// jury_size is committee-editable, so the seated-jury floor has to be
	// enforced here as well — otherwise an operational-params update could
	// disable the redraw sweep entirely without a governance vote.
	if op.JurySize <= MinSeatedJurors {
		return fmt.Errorf("jury size must exceed the seated-jury floor %d, got %d",
			MinSeatedJurors, op.JurySize)
	}
	// Jury super-majority must be in (0,1]; >1 deadlocks every jury/appeal.
	if op.JurySuperMajority.IsNil() || !op.JurySuperMajority.IsPositive() || op.JurySuperMajority.GT(math.LegacyOneDec()) {
		return fmt.Errorf("jury super majority must be in (0,1]: %s", op.JurySuperMajority)
	}
	// Review/challenge windows must be positive; 0 yields an instant deadline.
	if op.DefaultReviewPeriodEpochs <= 0 {
		return fmt.Errorf("default review period epochs must be positive: %d", op.DefaultReviewPeriodEpochs)
	}
	if op.DefaultChallengePeriodEpochs <= 0 {
		return fmt.Errorf("default challenge period epochs must be positive: %d", op.DefaultChallengePeriodEpochs)
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
	if op.JurorRewardRate.IsNil() || op.JurorRewardRate.IsNegative() ||
		op.JurorRewardRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("juror reward rate must be in [0,1]: %s", op.JurorRewardRate)
	}
	if op.ReviewerBondReserveRate.IsNil() || !op.ReviewerBondReserveRate.IsPositive() ||
		op.ReviewerBondReserveRate.GT(math.LegacyOneDec()) {
		// Zero would let a reviewer approve a mint with nothing at risk, which
		// is the entire accountability of the role.
		return fmt.Errorf("reviewer bond reserve rate must be in (0,1]: %s", op.ReviewerBondReserveRate)
	}
	if err := op.ReviewerBondPolicy().Validate(); err != nil {
		return err
	}
	if op.ReviewFeeRate.IsNil() || op.ReviewFeeRate.IsNegative() ||
		op.ReviewFeeRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("review fee rate must be in [0,1]: %s", op.ReviewFeeRate)
	}
	if op.MaxReviewerRewardPool.IsNil() || op.MaxReviewerRewardPool.IsNegative() {
		return fmt.Errorf("max reviewer reward pool must be non-negative: %s", op.MaxReviewerRewardPool)
	}
	if op.MaxReviewerRewardPool.GT(RoleRewardPoolCeiling()) {
		return fmt.Errorf("max reviewer reward pool exceeds ceiling %s: %s", RoleRewardPoolCeiling(), op.MaxReviewerRewardPool)
	}
	if op.ReviewerRewardPoolOverflowBurnRatio.IsNil() || op.ReviewerRewardPoolOverflowBurnRatio.IsNegative() ||
		op.ReviewerRewardPoolOverflowBurnRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("reviewer reward pool overflow burn ratio must be in [0,1]: %s", op.ReviewerRewardPoolOverflowBurnRatio)
	}
	if op.ReviewerRewardEpochBlocks == 0 {
		// Zero would divide by zero when deriving the epoch number.
		return fmt.Errorf("reviewer reward epoch blocks must be positive")
	}
	// Zero is meaningful here: it disables automatic funding entirely and
	// returns the pools to manual top-ups. The upper bound is 1 — a share
	// above the community pool's whole inflation income would mean x/rep
	// intends to leave the councils nothing, which should not be expressible.
	if op.RoleRewardInflationShare.IsNil() || op.RoleRewardInflationShare.IsNegative() ||
		op.RoleRewardInflationShare.GT(math.LegacyOneDec()) {
		return fmt.Errorf("role reward inflation share must be in [0,1]: %s", op.RoleRewardInflationShare)
	}
	if op.MaxCuratorRewardPool.IsNil() || op.MaxCuratorRewardPool.IsNegative() {
		return fmt.Errorf("max curator reward pool must be non-negative: %s", op.MaxCuratorRewardPool)
	}
	if op.MaxCuratorRewardPool.GT(RoleRewardPoolCeiling()) {
		return fmt.Errorf("max curator reward pool exceeds ceiling %s: %s", RoleRewardPoolCeiling(), op.MaxCuratorRewardPool)
	}
	if op.CuratorRewardPoolOverflowBurnRatio.IsNil() || op.CuratorRewardPoolOverflowBurnRatio.IsNegative() ||
		op.CuratorRewardPoolOverflowBurnRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("curator reward pool overflow burn ratio must be in [0,1]: %s", op.CuratorRewardPoolOverflowBurnRatio)
	}
	if op.CuratorRewardEpochBlocks == 0 {
		// Zero would divide by zero when deriving the epoch number.
		return fmt.Errorf("curator reward epoch blocks must be positive")
	}
	if op.MinCuratorAccuracy.IsNil() || op.MinCuratorAccuracy.IsNegative() ||
		op.MinCuratorAccuracy.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min curator accuracy must be in [0,1]: %s", op.MinCuratorAccuracy)
	}
	if op.CuratorAccuracyWindowEpochs == 0 {
		return fmt.Errorf("curator accuracy window epochs must be positive")
	}
	if op.MaxVerifierRewardPool.IsNil() || op.MaxVerifierRewardPool.IsNegative() {
		return fmt.Errorf("max verifier reward pool must be non-negative: %s", op.MaxVerifierRewardPool)
	}
	if op.MaxVerifierRewardPool.GT(RoleRewardPoolCeiling()) {
		return fmt.Errorf("max verifier reward pool exceeds ceiling %s: %s", RoleRewardPoolCeiling(), op.MaxVerifierRewardPool)
	}
	if op.VerifierRewardPoolOverflowBurnRatio.IsNil() || op.VerifierRewardPoolOverflowBurnRatio.IsNegative() ||
		op.VerifierRewardPoolOverflowBurnRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("verifier reward pool overflow burn ratio must be in [0,1]: %s", op.VerifierRewardPoolOverflowBurnRatio)
	}
	if op.VerifierRewardEpochBlocks == 0 {
		return fmt.Errorf("verifier reward epoch blocks must be positive")
	}
	if op.MinVerifierAccuracy.IsNil() || op.MinVerifierAccuracy.IsNegative() ||
		op.MinVerifierAccuracy.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min verifier accuracy must be in [0,1]: %s", op.MinVerifierAccuracy)
	}
	if op.VerifierAccuracyWindowEpochs == 0 {
		return fmt.Errorf("verifier accuracy window epochs must be positive")
	}
	if op.VerifierDreamReward.IsNil() || op.VerifierDreamReward.IsNegative() {
		return fmt.Errorf("verifier dream reward must be non-negative: %s", op.VerifierDreamReward)
	}
	if op.MaxVerifierDreamMintPerEpoch.IsNil() || op.MaxVerifierDreamMintPerEpoch.IsNegative() {
		return fmt.Errorf("max verifier dream mint per epoch must be non-negative: %s", op.MaxVerifierDreamMintPerEpoch)
	}
	// Zero is meaningful: it disables the chain-wide gate and leaves review to
	// per-project policy.
	if op.ReviewRequiredAboveBudget.IsNil() || op.ReviewRequiredAboveBudget.IsNegative() {
		return fmt.Errorf("review required above budget must be non-negative: %s", op.ReviewRequiredAboveBudget)
	}
	if op.PermissionlessMinReviewBountyRate.IsNil() || op.PermissionlessMinReviewBountyRate.IsNegative() ||
		op.PermissionlessMinReviewBountyRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("permissionless min review bounty rate must be in [0,1]: %s", op.PermissionlessMinReviewBountyRate)
	}
	if op.MinReviewerAccuracy.IsNil() || op.MinReviewerAccuracy.IsNegative() ||
		op.MinReviewerAccuracy.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min reviewer accuracy must be in [0,1]: %s", op.MinReviewerAccuracy)
	}
	if op.ReviewerAccuracyWindowEpochs == 0 || op.ReviewerAccuracyWindowEpochs > MaxSentinelAccuracyWindowEpochs {
		return fmt.Errorf("reviewer accuracy window epochs must be in [1,%d]: %d",
			MaxSentinelAccuracyWindowEpochs, op.ReviewerAccuracyWindowEpochs)
	}
	if op.MaxReviewRounds == 0 {
		// Zero rounds means a rejection can never be remedied and the work is
		// stuck; one round is the minimum that still allows a resubmission.
		return fmt.Errorf("max review rounds must be at least 1")
	}
	if op.MinJurorReward.IsNil() || op.MinJurorReward.IsNegative() {
		return fmt.Errorf("min juror reward must be non-negative: %s", op.MinJurorReward)
	}
	if op.MinJurorSelectionWeight.IsNil() || !op.MinJurorSelectionWeight.IsPositive() ||
		op.MinJurorSelectionWeight.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min juror selection weight must be in (0,1]: %s", op.MinJurorSelectionWeight)
	}
	if op.InitiativeCompletionBonusRate.IsNil() || op.InitiativeCompletionBonusRate.IsNegative() ||
		op.InitiativeCompletionBonusRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("initiative completion bonus rate must be in [0,1]: %s",
			op.InitiativeCompletionBonusRate)
	}
	if op.JuryAcceptanceWindowRatio.IsNil() || !op.JuryAcceptanceWindowRatio.IsPositive() ||
		op.JuryAcceptanceWindowRatio.GTE(math.LegacyOneDec()) {
		return fmt.Errorf("jury acceptance window ratio must be in (0,1): %s", op.JuryAcceptanceWindowRatio)
	}
	// The window and the redraw rounds are tuned together and must fit inside
	// the review period together, so the coupling is enforced on this surface
	// too — otherwise a committee update could satisfy each field in isolation
	// and still leave replacement jurors no time to read the work.
	if op.JuryAcceptanceWindowRatio.MulInt64(int64(op.MaxJuryRedraws + 1)).GTE(math.LegacyOneDec()) {
		return fmt.Errorf(
			"jury acceptance window ratio %s x %d redraw rounds consumes the whole review period",
			op.JuryAcceptanceWindowRatio, op.MaxJuryRedraws+1)
	}
	if op.AbandonedJurySeatPenalty.IsNil() || op.AbandonedJurySeatPenalty.IsNegative() {
		return fmt.Errorf("abandoned jury seat penalty cannot be negative: %s", op.AbandonedJurySeatPenalty)
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
	// Anti-whale staking cap
	if !op.MaxInitiativeStakePerMember.IsPositive() {
		return fmt.Errorf("max initiative stake per member must be positive: %s", op.MaxInitiativeStakePerMember)
	}
	// Anti-collusion caps
	if !op.MaxInitiativeRewardsPerSeason.IsPositive() {
		return fmt.Errorf("max initiative rewards per season must be positive: %s", op.MaxInitiativeRewardsPerSeason)
	}
	if !op.LargeProjectBudgetThreshold.IsPositive() {
		return fmt.Errorf("large project budget threshold must be positive: %s", op.LargeProjectBudgetThreshold)
	}
	// Permissionless creation fees
	if op.ProjectCreationFee.IsNegative() {
		return fmt.Errorf("project creation fee cannot be negative: %s", op.ProjectCreationFee)
	}
	if op.InitiativeCreationFeeApprentice.IsNegative() {
		return fmt.Errorf("initiative creation fee (apprentice) cannot be negative: %s", op.InitiativeCreationFeeApprentice)
	}
	if op.InitiativeCreationFeeStandard.IsNegative() {
		return fmt.Errorf("initiative creation fee (standard) cannot be negative: %s", op.InitiativeCreationFeeStandard)
	}
	if op.TagCreationFee.IsNegative() {
		return fmt.Errorf("tag creation fee cannot be negative: %s", op.TagCreationFee)
	}
	// Sentinel reward pool validation
	if op.MaxSentinelRewardPool.IsNegative() {
		return fmt.Errorf("max sentinel reward pool cannot be negative: %s", op.MaxSentinelRewardPool)
	}
	if !op.MaxSentinelRewardPool.IsNil() && op.MaxSentinelRewardPool.GT(RoleRewardPoolCeiling()) {
		return fmt.Errorf("max sentinel reward pool exceeds ceiling %s: %s", RoleRewardPoolCeiling(), op.MaxSentinelRewardPool)
	}
	if op.SentinelRewardPoolOverflowBurnRatio.IsNegative() {
		return fmt.Errorf("sentinel reward pool overflow burn ratio cannot be negative: %s", op.SentinelRewardPoolOverflowBurnRatio)
	}
	if op.SentinelRewardPoolOverflowBurnRatio.GT(math.LegacyOneDec()) {
		return fmt.Errorf("sentinel reward pool overflow burn ratio cannot be greater than 1: %s", op.SentinelRewardPoolOverflowBurnRatio)
	}
	if op.SentinelRewardEpochBlocks == 0 {
		return fmt.Errorf("sentinel reward epoch blocks must be positive")
	}
	if op.MinSentinelAccuracy.IsNegative() || op.MinSentinelAccuracy.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min sentinel accuracy must be in [0,1]: %s", op.MinSentinelAccuracy)
	}
	if op.MinAppealRate.IsNegative() || op.MinAppealRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min appeal rate must be in [0,1]: %s", op.MinAppealRate)
	}
	if op.SentinelAccuracyWindowEpochs == 0 || op.SentinelAccuracyWindowEpochs > MaxSentinelAccuracyWindowEpochs {
		return fmt.Errorf("sentinel accuracy window epochs must be in [1,%d]: %d", MaxSentinelAccuracyWindowEpochs, op.SentinelAccuracyWindowEpochs)
	}
	if op.MaxDreamMintPerEpoch.IsNil() || op.MaxDreamMintPerEpoch.IsZero() || op.MaxDreamMintPerEpoch.IsNegative() {
		return fmt.Errorf("max_dream_mint_per_epoch must be positive")
	}
	// Proposal-time caps: see Params.Validate() for rationale.
	if !op.MaxProjectRequestedBudget.IsPositive() {
		return fmt.Errorf("max project requested budget must be positive: %s", op.MaxProjectRequestedBudget)
	}
	if !op.MaxProjectRequestedSpark.IsPositive() {
		return fmt.Errorf("max project requested spark must be positive: %s", op.MaxProjectRequestedSpark)
	}
	if op.ProposedProjectExpiryBlocks <= 0 {
		return fmt.Errorf("proposed project expiry blocks must be positive: %d", op.ProposedProjectExpiryBlocks)
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
	p.UnstakedDecayRate = op.UnstakedDecayRate
	p.StakedDecayRate = op.StakedDecayRate
	p.NewMemberDecayGraceEpochs = op.NewMemberDecayGraceEpochs
	p.TransferTaxRate = op.TransferTaxRate
	p.MaxTipAmount = op.MaxTipAmount
	p.MaxTipsPerEpoch = op.MaxTipsPerEpoch
	p.MaxGiftAmount = op.MaxGiftAmount
	p.GiftOnlyToInvitees = op.GiftOnlyToInvitees
	// Seasonal staking reward pool
	p.MaxStakingRewardsPerSeason = op.MaxStakingRewardsPerSeason
	// Treasury management
	p.MaxTreasuryBalance = op.MaxTreasuryBalance
	p.TreasuryFundsInterims = op.TreasuryFundsInterims
	p.TreasuryFundsRetroPgf = op.TreasuryFundsRetroPgf
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
	p.AbandonedJurySeatPenalty = op.AbandonedJurySeatPenalty
	p.JurorRewardRate = op.JurorRewardRate
	p.MinJurorReward = op.MinJurorReward
	p.MinJurorSelectionWeight = op.MinJurorSelectionWeight
	p.MinJurySeatingsForWeighting = op.MinJurySeatingsForWeighting
	p.InitiativeCompletionBonusRate = op.InitiativeCompletionBonusRate
	p.JuryAcceptanceWindowRatio = op.JuryAcceptanceWindowRatio
	p.MaxJuryRedraws = op.MaxJuryRedraws
	p.ReviewerBondReserveRate = op.ReviewerBondReserveRate
	p.ReviewFeeRate = op.ReviewFeeRate
	p.MaxReviewRounds = op.MaxReviewRounds
	// Reviewer bonded-role policy. The caller writes these through to the
	// BondedRoleConfig after the merge (SyncReviewerBondedRoleConfig); merging
	// here alone would change the params without changing what BondRole enforces.
	p.MinReviewerBond = op.MinReviewerBond
	p.ReviewerDemotionThreshold = op.ReviewerDemotionThreshold
	p.MinReviewerTrustLevel = op.MinReviewerTrustLevel
	p.MinReviewerRepTier = op.MinReviewerRepTier
	p.MinReviewerAgeBlocks = op.MinReviewerAgeBlocks
	p.ReviewerDemotionCooldown = op.ReviewerDemotionCooldown
	p.ReviewerUnbondCooldown = op.ReviewerUnbondCooldown
	// Anti-whale staking cap
	p.MaxInitiativeStakePerMember = op.MaxInitiativeStakePerMember
	// Anti-collusion caps
	p.MaxInitiativeRewardsPerSeason = op.MaxInitiativeRewardsPerSeason
	p.LargeProjectBudgetThreshold = op.LargeProjectBudgetThreshold
	// Permissionless creation fees
	p.ProjectCreationFee = op.ProjectCreationFee
	p.InitiativeCreationFeeApprentice = op.InitiativeCreationFeeApprentice
	p.InitiativeCreationFeeStandard = op.InitiativeCreationFeeStandard
	p.TagCreationFee = op.TagCreationFee
	// Sentinel SPARK reward pool
	p.MaxSentinelRewardPool = op.MaxSentinelRewardPool
	p.MaxReviewerRewardPool = op.MaxReviewerRewardPool
	p.ReviewerRewardPoolOverflowBurnRatio = op.ReviewerRewardPoolOverflowBurnRatio
	p.ReviewerRewardEpochBlocks = op.ReviewerRewardEpochBlocks
	p.MinReviewerAccuracy = op.MinReviewerAccuracy
	p.ReviewerAccuracyWindowEpochs = op.ReviewerAccuracyWindowEpochs
	p.RoleRewardInflationShare = op.RoleRewardInflationShare
	p.MaxCuratorRewardPool = op.MaxCuratorRewardPool
	p.CuratorRewardPoolOverflowBurnRatio = op.CuratorRewardPoolOverflowBurnRatio
	p.CuratorRewardEpochBlocks = op.CuratorRewardEpochBlocks
	p.MinCuratorAccuracy = op.MinCuratorAccuracy
	p.CuratorAccuracyWindowEpochs = op.CuratorAccuracyWindowEpochs
	p.MaxVerifierRewardPool = op.MaxVerifierRewardPool
	p.VerifierRewardPoolOverflowBurnRatio = op.VerifierRewardPoolOverflowBurnRatio
	p.VerifierRewardEpochBlocks = op.VerifierRewardEpochBlocks
	p.MinVerifierAccuracy = op.MinVerifierAccuracy
	p.VerifierAccuracyWindowEpochs = op.VerifierAccuracyWindowEpochs
	p.MinEpochVerifications = op.MinEpochVerifications
	p.VerifierDreamReward = op.VerifierDreamReward
	p.MaxVerifierDreamMintPerEpoch = op.MaxVerifierDreamMintPerEpoch
	p.ReviewRequiredAboveBudget = op.ReviewRequiredAboveBudget
	p.ReviewBountyReclaimDelay = op.ReviewBountyReclaimDelay
	p.PermissionlessMinReviewBountyRate = op.PermissionlessMinReviewBountyRate
	p.SentinelRewardPoolOverflowBurnRatio = op.SentinelRewardPoolOverflowBurnRatio
	p.SentinelRewardEpochBlocks = op.SentinelRewardEpochBlocks
	p.MinSentinelAccuracy = op.MinSentinelAccuracy
	p.MinAppealsForAccuracy = op.MinAppealsForAccuracy
	p.MinEpochActivityForReward = op.MinEpochActivityForReward
	p.MinAppealRate = op.MinAppealRate
	p.SentinelAccuracyWindowEpochs = op.SentinelAccuracyWindowEpochs
	// Per-member active work caps
	p.MaxActiveInitiativesPerMember = op.MaxActiveInitiativesPerMember
	p.MaxActiveInterimsPerMember = op.MaxActiveInterimsPerMember
	// DREAM emission cap
	p.MaxDreamMintPerEpoch = op.MaxDreamMintPerEpoch
	// Proposal-time hard caps and PROPOSED-project expiry
	p.MaxProjectRequestedBudget = op.MaxProjectRequestedBudget
	p.MaxProjectRequestedSpark = op.MaxProjectRequestedSpark
	p.ProposedProjectExpiryBlocks = op.ProposedProjectExpiryBlocks
	return p
}

// ExtractOperationalParams extracts the operational fields from Params into RepOperationalParams.
func (p Params) ExtractOperationalParams() RepOperationalParams {
	return RepOperationalParams{
		// Time
		EpochBlocks:          p.EpochBlocks,
		SeasonDurationEpochs: p.SeasonDurationEpochs,
		// DREAM economics
		UnstakedDecayRate:         p.UnstakedDecayRate,
		StakedDecayRate:           p.StakedDecayRate,
		NewMemberDecayGraceEpochs: p.NewMemberDecayGraceEpochs,
		TransferTaxRate:           p.TransferTaxRate,
		MaxTipAmount:              p.MaxTipAmount,
		MaxTipsPerEpoch:           p.MaxTipsPerEpoch,
		MaxGiftAmount:             p.MaxGiftAmount,
		GiftOnlyToInvitees:        p.GiftOnlyToInvitees,
		// Seasonal staking reward pool
		MaxStakingRewardsPerSeason: p.MaxStakingRewardsPerSeason,
		// Treasury management
		MaxTreasuryBalance:    p.MaxTreasuryBalance,
		TreasuryFundsInterims: p.TreasuryFundsInterims,
		TreasuryFundsRetroPgf: p.TreasuryFundsRetroPgf,
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
		ReputationDecayRate:           p.ReputationDecayRate,
		MaxConvictionSharePerMember:   p.MaxConvictionSharePerMember,
		InvitationStakeBurnRate:       p.InvitationStakeBurnRate,
		MaxReputationGainPerEpoch:     p.MaxReputationGainPerEpoch,
		AbandonedJurySeatPenalty:      p.AbandonedJurySeatPenalty,
		JurorRewardRate:               p.JurorRewardRate,
		MinJurorReward:                p.MinJurorReward,
		MinJurorSelectionWeight:       p.MinJurorSelectionWeight,
		MinJurySeatingsForWeighting:   p.MinJurySeatingsForWeighting,
		InitiativeCompletionBonusRate: p.InitiativeCompletionBonusRate,
		JuryAcceptanceWindowRatio:     p.JuryAcceptanceWindowRatio,
		MaxJuryRedraws:                p.MaxJuryRedraws,
		ReviewerBondReserveRate:       p.ReviewerBondReserveRate,
		ReviewFeeRate:                 p.ReviewFeeRate,
		MaxReviewRounds:               p.MaxReviewRounds,
		MinReviewerBond:               p.MinReviewerBond,
		ReviewerDemotionThreshold:     p.ReviewerDemotionThreshold,
		MinReviewerTrustLevel:         p.MinReviewerTrustLevel,
		MinReviewerRepTier:            p.MinReviewerRepTier,
		MinReviewerAgeBlocks:          p.MinReviewerAgeBlocks,
		ReviewerDemotionCooldown:      p.ReviewerDemotionCooldown,
		ReviewerUnbondCooldown:        p.ReviewerUnbondCooldown,
		// Anti-whale staking cap
		MaxInitiativeStakePerMember: p.MaxInitiativeStakePerMember,
		// Anti-collusion caps
		MaxInitiativeRewardsPerSeason: p.MaxInitiativeRewardsPerSeason,
		LargeProjectBudgetThreshold:   p.LargeProjectBudgetThreshold,
		// Permissionless creation fees
		ProjectCreationFee:              p.ProjectCreationFee,
		InitiativeCreationFeeApprentice: p.InitiativeCreationFeeApprentice,
		InitiativeCreationFeeStandard:   p.InitiativeCreationFeeStandard,
		TagCreationFee:                  p.TagCreationFee,
		// Sentinel SPARK reward pool
		MaxSentinelRewardPool:               p.MaxSentinelRewardPool,
		SentinelRewardPoolOverflowBurnRatio: p.SentinelRewardPoolOverflowBurnRatio,
		MaxReviewerRewardPool:               p.MaxReviewerRewardPool,
		ReviewerRewardPoolOverflowBurnRatio: p.ReviewerRewardPoolOverflowBurnRatio,
		ReviewerRewardEpochBlocks:           p.ReviewerRewardEpochBlocks,
		MinReviewerAccuracy:                 p.MinReviewerAccuracy,
		ReviewerAccuracyWindowEpochs:        p.ReviewerAccuracyWindowEpochs,
		RoleRewardInflationShare:            p.RoleRewardInflationShare,
		MaxCuratorRewardPool:                p.MaxCuratorRewardPool,
		CuratorRewardPoolOverflowBurnRatio:  p.CuratorRewardPoolOverflowBurnRatio,
		CuratorRewardEpochBlocks:            p.CuratorRewardEpochBlocks,
		MinCuratorAccuracy:                  p.MinCuratorAccuracy,
		CuratorAccuracyWindowEpochs:         p.CuratorAccuracyWindowEpochs,
		MaxVerifierRewardPool:               p.MaxVerifierRewardPool,
		VerifierRewardPoolOverflowBurnRatio: p.VerifierRewardPoolOverflowBurnRatio,
		VerifierRewardEpochBlocks:           p.VerifierRewardEpochBlocks,
		MinVerifierAccuracy:                 p.MinVerifierAccuracy,
		VerifierAccuracyWindowEpochs:        p.VerifierAccuracyWindowEpochs,
		MinEpochVerifications:               p.MinEpochVerifications,
		VerifierDreamReward:                 p.VerifierDreamReward,
		MaxVerifierDreamMintPerEpoch:        p.MaxVerifierDreamMintPerEpoch,
		ReviewRequiredAboveBudget:           p.ReviewRequiredAboveBudget,
		ReviewBountyReclaimDelay:            p.ReviewBountyReclaimDelay,
		PermissionlessMinReviewBountyRate:   p.PermissionlessMinReviewBountyRate,
		SentinelRewardEpochBlocks:           p.SentinelRewardEpochBlocks,
		MinSentinelAccuracy:                 p.MinSentinelAccuracy,
		MinAppealsForAccuracy:               p.MinAppealsForAccuracy,
		MinEpochActivityForReward:           p.MinEpochActivityForReward,
		MinAppealRate:                       p.MinAppealRate,
		SentinelAccuracyWindowEpochs:        p.SentinelAccuracyWindowEpochs,
		// Per-member active work caps
		MaxActiveInitiativesPerMember: p.MaxActiveInitiativesPerMember,
		MaxActiveInterimsPerMember:    p.MaxActiveInterimsPerMember,
		// DREAM emission cap
		MaxDreamMintPerEpoch: p.MaxDreamMintPerEpoch,
		// Proposal-time hard caps and PROPOSED-project expiry
		MaxProjectRequestedBudget:   p.MaxProjectRequestedBudget,
		MaxProjectRequestedSpark:    p.MaxProjectRequestedSpark,
		ProposedProjectExpiryBlocks: p.ProposedProjectExpiryBlocks,
	}
}

// ReviewerBondPolicy is the reviewer bonded-role config as it lives in params:
// the seven fields Params and RepOperationalParams both carry and that
// SyncReviewerBondedRoleConfig projects onto the BondedRoleConfig for
// ROLE_TYPE_INITIATIVE_REVIEWER. Grouping them keeps the validator and the
// write-through reading from one shape instead of seven positional arguments,
// three of which are adjacent int64s.
type ReviewerBondPolicy struct {
	MinBond           math.Int
	DemotionThreshold math.Int
	MinTrustLevel     string
	MinRepTier        uint64
	MinAgeBlocks      int64
	DemotionCooldown  int64
	UnbondCooldown    int64
}

// ReviewerBondPolicy returns the reviewer bonded-role fields.
func (p Params) ReviewerBondPolicy() ReviewerBondPolicy {
	return ReviewerBondPolicy{
		MinBond:           p.MinReviewerBond,
		DemotionThreshold: p.ReviewerDemotionThreshold,
		MinTrustLevel:     p.MinReviewerTrustLevel,
		MinRepTier:        p.MinReviewerRepTier,
		MinAgeBlocks:      p.MinReviewerAgeBlocks,
		DemotionCooldown:  p.ReviewerDemotionCooldown,
		UnbondCooldown:    p.ReviewerUnbondCooldown,
	}
}

// ReviewerBondPolicy returns the reviewer bonded-role fields.
func (op RepOperationalParams) ReviewerBondPolicy() ReviewerBondPolicy {
	return ReviewerBondPolicy{
		MinBond:           op.MinReviewerBond,
		DemotionThreshold: op.ReviewerDemotionThreshold,
		MinTrustLevel:     op.MinReviewerTrustLevel,
		MinRepTier:        op.MinReviewerRepTier,
		MinAgeBlocks:      op.MinReviewerAgeBlocks,
		DemotionCooldown:  op.ReviewerDemotionCooldown,
		UnbondCooldown:    op.ReviewerUnbondCooldown,
	}
}

// Validate checks the reviewer bonded-role fields shared by Params and
// RepOperationalParams.
//
// Every field is required and fully constrained here, because the write-through
// to the BondedRoleConfig is a straight projection: whatever survives this is
// what BondRole enforces, with no defaulting in between. A field left to its
// zero value would otherwise be indistinguishable from a deliberate zero and
// would quietly loosen the role.
func (rp ReviewerBondPolicy) Validate() error {
	if rp.MinBond.IsNil() || rp.MinBond.IsNegative() {
		return fmt.Errorf("min reviewer bond must be non-negative: %s", rp.MinBond)
	}
	if rp.DemotionThreshold.IsNil() || rp.DemotionThreshold.IsNegative() {
		return fmt.Errorf("reviewer demotion threshold must be non-negative: %s", rp.DemotionThreshold)
	}
	// A threshold above the floor would demote every reviewer the moment they
	// bonded the minimum, emptying the roster on the next sweep.
	if rp.DemotionThreshold.GT(rp.MinBond) {
		return fmt.Errorf("reviewer demotion threshold %s must not exceed min reviewer bond %s", rp.DemotionThreshold, rp.MinBond)
	}
	// Unlike the sentinel and curator, an empty trust level is rejected rather
	// than read as "no gate". BondRole skips the check entirely on an empty
	// string, so omission would open the one role whose approvals mint DREAM.
	// An ungated reviewer roster is still expressible -- as TRUST_LEVEL_NEW --
	// it just has to be said out loud.
	if rp.MinTrustLevel == "" {
		return fmt.Errorf("min reviewer trust level must be set (use TRUST_LEVEL_NEW for no gate)")
	}
	if _, ok := TrustLevel_value[rp.MinTrustLevel]; !ok {
		return fmt.Errorf("invalid min reviewer trust level: %s", rp.MinTrustLevel)
	}
	if rp.MinRepTier > 5 {
		return fmt.Errorf("min reviewer rep tier must be 0-5: %d", rp.MinRepTier)
	}
	if rp.MinAgeBlocks < 0 {
		return fmt.Errorf("min reviewer age blocks must be non-negative: %d", rp.MinAgeBlocks)
	}
	if rp.DemotionCooldown < 0 {
		return fmt.Errorf("reviewer demotion cooldown must be non-negative: %d", rp.DemotionCooldown)
	}
	if rp.UnbondCooldown < 0 {
		return fmt.Errorf("reviewer unbond cooldown must be non-negative: %d", rp.UnbondCooldown)
	}
	return nil
}
