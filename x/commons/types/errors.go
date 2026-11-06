package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/commons module sentinel errors
var (
	ErrInvalidSigner = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")

	// Governance & Group Errors
	ErrGroupNotFound     = errors.Register(ModuleName, 1600, "group not found in registry")
	ErrInvalidGroupSize  = errors.Register(ModuleName, 1601, "group member count is outside defined bounds")
	ErrRateLimitExceeded = errors.Register(ModuleName, 1602, "spending limit or update rate limit exceeded")
	ErrGroupNotActive    = errors.Register(ModuleName, 1603, "group is not yet active (shell group)")
	ErrGroupExpired      = errors.Register(ModuleName, 1604, "group term has expired")

	ErrInvalidMemberWeight = errors.Register(ModuleName, 1605, "invalid member weight")
	ErrGoldenShareSeat     = errors.Register(ModuleName, 1606, "the golden-share seat can only be changed by x/gov")
	ErrBallotMode          = errors.Register(ModuleName, 1607, "the ballot mode does not allow this vote")

	// Secret ballots and polls
	ErrSecretBallotUnavailable = errors.Register(ModuleName, 1608, "a secret ballot needs equal member weights and enough members with registered ZK keys")
	ErrPollNotFound            = errors.Register(ModuleName, 1609, "poll not found")
	ErrPollNotOpen             = errors.Register(ModuleName, 1610, "poll is not open for voting")
	ErrInvalidPoll             = errors.Register(ModuleName, 1611, "invalid poll")
	ErrPollLimit               = errors.Register(ModuleName, 1612, "too many open polls")
	ErrAlreadyVoted            = errors.Register(ModuleName, 1613, "already voted on this poll")
	ErrPollHasVotes            = errors.Register(ModuleName, 1614, "a poll with votes cannot be cancelled")
	// A group that is another group's parent or electoral authority cannot
	// be deleted: the dependent would keep a policy that no longer exists.
	ErrGroupHasDependents = errors.Register(ModuleName, 1615, "group is the parent or electoral authority of another group")
	// ErrPollRateLimit: the creator opened MaxPollOpensPerCreatorPerDay polls
	// today already; cancelling one does not give the slot back.
	ErrPollRateLimit = errors.Register(ModuleName, 1616, "too many polls opened today")
	// ErrInsufficientProposalFee: the proposer cannot pay Params.proposal_fee
	// for a MsgSubmitProposal the ante did not floor (a nested submission).
	ErrInsufficientProposalFee = errors.Register(ModuleName, 1617, "proposer cannot pay the proposal fee")

	// Recurring Spend Errors
	ErrRecurringSpendNotFound      = errors.Register(ModuleName, 1700, "recurring spend schedule not found")
	ErrRecurringSpendInactive      = errors.Register(ModuleName, 1701, "recurring spend schedule is not active")
	ErrRecurringSpendNotDue        = errors.Register(ModuleName, 1702, "recurring spend period has not elapsed")
	ErrRecurringSpendWindowClosed  = errors.Register(ModuleName, 1703, "recurring spend window has closed (past end_time)")
	ErrRecurringSpendInvalidPeriod = errors.Register(ModuleName, 1704, "recurring spend period is invalid")
	ErrRecurringSpendInvalidWindow = errors.Register(ModuleName, 1705, "recurring spend start/end window is invalid")
	ErrRecurringSpendCapReached    = errors.Register(ModuleName, 1706, "authority has reached max active recurring spends")
	ErrRecurringSpendUnauthorized  = errors.Register(ModuleName, 1707, "caller is not authorized for this recurring spend")
)
