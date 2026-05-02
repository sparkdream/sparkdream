package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// Time-denominated deadlines are stored as block counts. Conversion assumes ~5s
// block times (matching x/season's DefaultEpochBlocks), so 1 day ≈ 17,280 blocks.
//
// DREAM-denominated amounts are stored in micro-DREAM (udream); 1 DREAM = 1e6 udream.
var (
	DefaultStakeDeadlineEpochs      int64 = 1_036_800 // ~2 months  (60 days at 5s blocks)
	DefaultRevealDeadlineEpochs     int64 = 241_920   // ~2 weeks after backed
	DefaultVerificationPeriodEpochs int64 = 241_920   // ~2 weeks
	DefaultDisputeResolutionEpochs  int64 = 518_400   // ~1 month for council

	DefaultVerificationThreshold        = math.LegacyNewDecWithPrec(60, 2) // 60%
	DefaultMinVerificationVotes  uint32 = 3                                // base minimum; effective = max(3, stake_threshold/5000 DREAM)

	DefaultMaxTranches           uint32 = 10
	DefaultMaxTrancheValuation          = math.NewInt(50_000_000_000)      // 50,000 DREAM (50K * 1e6 udream)
	DefaultBondRate                     = math.LegacyNewDecWithPrec(10, 2) // 10% of total valuation
	DefaultMinProposerTrustLevel uint32 = 2                                // TRUST_LEVEL_ESTABLISHED

	DefaultMaxTotalValuation            = math.NewInt(50_000_000_000)      // 50,000 DREAM cap per contribution
	DefaultMinStakeAmount               = math.NewInt(100_000_000)         // 100 DREAM minimum per stake
	DefaultPayoutHoldbackRate           = math.LegacyNewDecWithPrec(20, 2) // 20% held back per tranche
	DefaultProposalCooldownEpochs int64 = 241_920                          // ~2 weeks after rejection
)

// NewParams creates a new Params instance.
func NewParams(
	stakeDeadlineEpochs int64,
	revealDeadlineEpochs int64,
	verificationPeriodEpochs int64,
	disputeResolutionEpochs int64,
	verificationThreshold math.LegacyDec,
	minVerificationVotes uint32,
	maxTranches uint32,
	maxTrancheValuation math.Int,
	bondRate math.LegacyDec,
	minProposerTrustLevel uint32,
	maxTotalValuation math.Int,
	minStakeAmount math.Int,
	payoutHoldbackRate math.LegacyDec,
	proposalCooldownEpochs int64,
) Params {
	return Params{
		StakeDeadlineEpochs:      stakeDeadlineEpochs,
		RevealDeadlineEpochs:     revealDeadlineEpochs,
		VerificationPeriodEpochs: verificationPeriodEpochs,
		DisputeResolutionEpochs:  disputeResolutionEpochs,
		VerificationThreshold:    verificationThreshold,
		MinVerificationVotes:     minVerificationVotes,
		MaxTranches:              maxTranches,
		MaxTrancheValuation:      maxTrancheValuation,
		BondRate:                 bondRate,
		MinProposerTrustLevel:    minProposerTrustLevel,
		MaxTotalValuation:        maxTotalValuation,
		MinStakeAmount:           minStakeAmount,
		PayoutHoldbackRate:       payoutHoldbackRate,
		ProposalCooldownEpochs:   proposalCooldownEpochs,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(
		DefaultStakeDeadlineEpochs,
		DefaultRevealDeadlineEpochs,
		DefaultVerificationPeriodEpochs,
		DefaultDisputeResolutionEpochs,
		DefaultVerificationThreshold,
		DefaultMinVerificationVotes,
		DefaultMaxTranches,
		DefaultMaxTrancheValuation,
		DefaultBondRate,
		DefaultMinProposerTrustLevel,
		DefaultMaxTotalValuation,
		DefaultMinStakeAmount,
		DefaultPayoutHoldbackRate,
		DefaultProposalCooldownEpochs,
	)
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if err := validateStakeDeadlineEpochs(p.StakeDeadlineEpochs); err != nil {
		return err
	}
	if err := validateRevealDeadlineEpochs(p.RevealDeadlineEpochs); err != nil {
		return err
	}
	if err := validateVerificationPeriodEpochs(p.VerificationPeriodEpochs); err != nil {
		return err
	}
	if err := validateDisputeResolutionEpochs(p.DisputeResolutionEpochs); err != nil {
		return err
	}
	if err := validateVerificationThreshold(p.VerificationThreshold); err != nil {
		return err
	}
	if err := validateMinVerificationVotes(p.MinVerificationVotes); err != nil {
		return err
	}
	if err := validateMaxTranches(p.MaxTranches); err != nil {
		return err
	}
	if err := validateMaxTrancheValuation(p.MaxTrancheValuation); err != nil {
		return err
	}
	if err := validateBondRate(p.BondRate); err != nil {
		return err
	}
	if err := validateMinProposerTrustLevel(p.MinProposerTrustLevel); err != nil {
		return err
	}
	if err := validateMaxTotalValuation(p.MaxTotalValuation); err != nil {
		return err
	}
	if err := validateMinStakeAmount(p.MinStakeAmount); err != nil {
		return err
	}
	if err := validatePayoutHoldbackRate(p.PayoutHoldbackRate); err != nil {
		return err
	}
	if err := validateProposalCooldownEpochs(p.ProposalCooldownEpochs); err != nil {
		return err
	}
	return nil
}

func validateStakeDeadlineEpochs(v int64) error {
	if v <= 0 {
		return fmt.Errorf("stake deadline epochs must be positive: %d", v)
	}
	return nil
}

func validateRevealDeadlineEpochs(v int64) error {
	if v <= 0 {
		return fmt.Errorf("reveal deadline epochs must be positive: %d", v)
	}
	return nil
}

func validateVerificationPeriodEpochs(v int64) error {
	if v <= 0 {
		return fmt.Errorf("verification period epochs must be positive: %d", v)
	}
	return nil
}

func validateDisputeResolutionEpochs(v int64) error {
	if v <= 0 {
		return fmt.Errorf("dispute resolution epochs must be positive: %d", v)
	}
	return nil
}

func validateVerificationThreshold(v math.LegacyDec) error {
	if v.IsNil() || !v.IsPositive() {
		return fmt.Errorf("verification threshold must be positive: %s", v)
	}
	if v.GT(math.LegacyOneDec()) {
		return fmt.Errorf("verification threshold must be <= 1: %s", v)
	}
	return nil
}

func validateMinVerificationVotes(v uint32) error {
	if v == 0 {
		return fmt.Errorf("min verification votes must be positive")
	}
	return nil
}

func validateMaxTranches(v uint32) error {
	if v == 0 {
		return fmt.Errorf("max tranches must be positive")
	}
	return nil
}

func validateMaxTrancheValuation(v math.Int) error {
	if v.IsNil() || !v.IsPositive() {
		return fmt.Errorf("max tranche valuation must be positive: %s", v)
	}
	return nil
}

func validateBondRate(v math.LegacyDec) error {
	if v.IsNil() || !v.IsPositive() {
		return fmt.Errorf("bond rate must be positive: %s", v)
	}
	if v.GT(math.LegacyOneDec()) {
		return fmt.Errorf("bond rate must be <= 1: %s", v)
	}
	return nil
}

func validateMinProposerTrustLevel(v uint32) error {
	if v == 0 {
		return fmt.Errorf("min proposer trust level must be positive")
	}
	return nil
}

func validateMaxTotalValuation(v math.Int) error {
	if v.IsNil() || !v.IsPositive() {
		return fmt.Errorf("max total valuation must be positive: %s", v)
	}
	return nil
}

func validateMinStakeAmount(v math.Int) error {
	if v.IsNil() || !v.IsPositive() {
		return fmt.Errorf("min stake amount must be positive: %s", v)
	}
	return nil
}

func validatePayoutHoldbackRate(v math.LegacyDec) error {
	if v.IsNil() || v.IsNegative() {
		return fmt.Errorf("payout holdback rate must be non-negative: %s", v)
	}
	if v.GTE(math.LegacyOneDec()) {
		return fmt.Errorf("payout holdback rate must be < 1: %s", v)
	}
	return nil
}

func validateProposalCooldownEpochs(v int64) error {
	if v < 0 {
		return fmt.Errorf("proposal cooldown epochs must be non-negative: %d", v)
	}
	return nil
}
