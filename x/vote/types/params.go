package types

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewParams creates a new Params instance.
func NewParams() Params {
	return Params{
		// ZK circuits (PLONK)
		SrsHash:              nil,
		VoteVerifyingKey:     nil,
		ProposalVerifyingKey: nil,
		TreeDepth:            20, // ~1M voters

		// Voting periods
		MinVotingPeriodEpochs:     3,
		MaxVotingPeriodEpochs:     30,
		DefaultVotingPeriodEpochs: 7,
		SealedRevealPeriodEpochs:  3,

		// Thresholds
		DefaultQuorum:        math.LegacyNewDecWithPrec(33, 2),  // 33%
		DefaultThreshold:     math.LegacyNewDecWithPrec(50, 2),  // 50%
		DefaultVetoThreshold: math.LegacyNewDecWithPrec(334, 3), // 33.4%

		// Registration
		OpenRegistration:     true,
		MinRegistrationStake: math.ZeroInt(),

		// Rate limiting
		MaxProposalsPerEpoch: 1,

		// Privacy
		AllowPrivateProposals:    true,
		AllowSealedProposals:     true,
		MaxPrivateEligibleVoters: 50,

		// Deposit
		MinProposalDeposit: sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),

		// Vote options
		MinVoteOptions: 2,
		MaxVoteOptions: 10,

		// TLE
		TleEnabled:              true,
		TleThresholdNumerator:   2,
		TleThresholdDenominator: 3,
		TleMasterPublicKey:      nil,
		MaxEncryptedRevealBytes: 512,
		TleMissWindow:           100,
		TleMissTolerance:        10,
		TleJailEnabled:          false,

		// Epoch fallback
		BlocksPerEpoch: 17280, // ~1 day at 5s blocks
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams()
}

// Validate validates the set of params.
func (p Params) Validate() error {
	return nil
}
