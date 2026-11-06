package keeper

import (
	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	// MaxVotingPeriodSeconds caps DecisionPolicy.voting_period (90 days).
	// Voting periods are no longer bounded by x/rep's ZK key delay
	// (CMN-101), so this is the only ceiling: it stops a typo from parking
	// a body's proposals for years (COM-203). Changing it is a chain
	// upgrade; the bootstrap windows are days, not months.
	MaxVotingPeriodSeconds int64 = 90 * 24 * 3600
	// MaxMinExecutionPeriodSeconds caps DecisionPolicy.min_execution_period
	// the same way.
	MaxMinExecutionPeriodSeconds int64 = 90 * 24 * 3600
)

// validateDecisionPeriods checks a decision policy's timing fields:
// voting_period in (0, MaxVotingPeriodSeconds], min_execution_period in
// [0, MaxMinExecutionPeriodSeconds].
func validateDecisionPeriods(votingPeriod, minExecutionPeriod int64) error {
	if votingPeriod <= 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "voting_period must be greater than 0")
	}
	if votingPeriod > MaxVotingPeriodSeconds {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "voting_period %d exceeds the maximum of %d seconds", votingPeriod, MaxVotingPeriodSeconds)
	}
	if minExecutionPeriod < 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "min_execution_period cannot be negative")
	}
	if minExecutionPeriod > MaxMinExecutionPeriodSeconds {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "min_execution_period %d exceeds the maximum of %d seconds", minExecutionPeriod, MaxMinExecutionPeriodSeconds)
	}
	return nil
}
