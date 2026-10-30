package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Proposal size caps: a proposal is stored forever and read by every voter.
const (
	MaxProposalMessages    = 20
	MaxProposalMetadataLen = 10_000
)

// checkProposalSize refuses an oversized proposal.
func checkProposalSize(msgs []*codectypes.Any, metadata string) error {
	if len(msgs) > MaxProposalMessages {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "a proposal carries at most %d messages, got %d", MaxProposalMessages, len(msgs))
	}
	if len(metadata) > MaxProposalMetadataLen {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "proposal metadata is at most %d bytes, got %d", MaxProposalMetadataLen, len(metadata))
	}
	return nil
}

// ProposalExecutionWindow is how long (seconds) an ACCEPTED proposal stays
// executable after its execution_time. Past it the EndBlocker marks the
// proposal EXPIRED, so an accepted-but-failed proposal cannot be run months
// later on the authority of a body whose members have since changed.
const ProposalExecutionWindow int64 = 14 * 24 * 3600

// executionDeadline is the last unix second at which an ACCEPTED proposal
// may execute.
func executionDeadline(p types.Proposal) int64 {
	return p.ExecutionTime + ProposalExecutionWindow
}

// queueDueTime returns the time at which the proposal next needs EndBlock
// attention, and whether it needs any.
func queueDueTime(p types.Proposal) (int64, bool) {
	switch p.Status {
	case types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED:
		return p.VotingDeadline, true
	case types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED:
		return executionDeadline(p), true
	default:
		return 0, false
	}
}

// enqueueProposal indexes an open proposal at its due time (no-op for a
// terminal one).
func (k Keeper) enqueueProposal(ctx context.Context, p types.Proposal) error {
	due, ok := queueDueTime(p)
	if !ok {
		return nil
	}
	return k.ProposalQueue.Set(ctx, collections.Join(due, p.Id))
}

// dequeueProposal removes the proposal's current queue entry (computed from
// its current status and times). Missing entries are ignored.
func (k Keeper) dequeueProposal(ctx context.Context, p types.Proposal) error {
	due, ok := queueDueTime(p)
	if !ok {
		return nil
	}
	return k.ProposalQueue.Remove(ctx, collections.Join(due, p.Id))
}
