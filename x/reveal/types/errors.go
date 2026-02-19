package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/reveal module sentinel errors
var (
	ErrInvalidSigner = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")

	// Contribution errors
	ErrContributionNotFound    = errors.Register(ModuleName, 1101, "contribution not found")
	ErrContributionNotProposed = errors.Register(ModuleName, 1102, "contribution is not in PROPOSED status")
	ErrNotInProgress           = errors.Register(ModuleName, 1103, "contribution is not in IN_PROGRESS status")

	// Tranche errors
	ErrTrancheNotFound    = errors.Register(ModuleName, 1110, "tranche not found")
	ErrTrancheNotStaking  = errors.Register(ModuleName, 1111, "tranche is not in STAKING status")
	ErrTrancheNotBacked   = errors.Register(ModuleName, 1112, "tranche is not in BACKED status")
	ErrTrancheNotRevealed = errors.Register(ModuleName, 1113, "tranche is not in REVEALED status")
	ErrTrancheNotDisputed = errors.Register(ModuleName, 1114, "tranche is not in DISPUTED status")

	// Stake errors
	ErrStakeNotFound         = errors.Register(ModuleName, 1120, "stake not found")
	ErrSelfStake             = errors.Register(ModuleName, 1121, "contributor cannot stake on own contribution")
	ErrStakeAmountTooLow     = errors.Register(ModuleName, 1122, "stake amount below minimum")
	ErrStakeExceedsThreshold = errors.Register(ModuleName, 1123, "stake would exceed tranche threshold")
	ErrWithdrawalNotAllowed  = errors.Register(ModuleName, 1124, "withdrawal not allowed during verification or dispute")

	// Vote errors
	ErrSelfVote             = errors.Register(ModuleName, 1130, "contributor cannot vote on own contribution")
	ErrNotStaker            = errors.Register(ModuleName, 1131, "only stakers for this tranche may vote")
	ErrAlreadyVoted         = errors.Register(ModuleName, 1132, "already voted on this tranche")
	ErrInvalidQualityRating = errors.Register(ModuleName, 1133, "quality rating must be between 1 and 5")

	// Access control errors
	ErrInsufficientTrustLevel = errors.Register(ModuleName, 1140, "insufficient trust level")
	ErrNotMember              = errors.Register(ModuleName, 1141, "address is not an active member")
	ErrNotContributor         = errors.Register(ModuleName, 1142, "only the contributor can perform this action")
	ErrUnauthorized           = errors.Register(ModuleName, 1143, "unauthorized")

	// Proposal validation errors
	ErrProposalCooldown        = errors.Register(ModuleName, 1150, "contributor is still in proposal cooldown")
	ErrTooManyTranches         = errors.Register(ModuleName, 1151, "too many tranches")
	ErrValuationTooHigh        = errors.Register(ModuleName, 1152, "total valuation exceeds maximum")
	ErrTrancheValuationTooHigh = errors.Register(ModuleName, 1153, "tranche valuation exceeds maximum")
	ErrValuationMismatch       = errors.Register(ModuleName, 1154, "sum of tranche thresholds must equal total valuation")
	ErrInsufficientBond        = errors.Register(ModuleName, 1155, "insufficient DREAM for bond")
	ErrEmptyProjectName        = errors.Register(ModuleName, 1156, "project name cannot be empty")
	ErrNoTranches              = errors.Register(ModuleName, 1157, "at least one tranche is required")

	// Dispute errors
	ErrInvalidVerdict = errors.Register(ModuleName, 1160, "verdict must be ACCEPT, IMPROVE, or REJECT")

	// Cancel errors
	ErrCannotCancelBacked = errors.Register(ModuleName, 1170, "contributor cannot cancel after a tranche has been backed or beyond")
)
