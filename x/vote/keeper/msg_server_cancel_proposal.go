package keeper

import (
	"bytes"
	"context"
	"fmt"

	"sparkdream/x/vote/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CancelProposal(ctx context.Context, msg *types.MsgCancelProposal) (*types.MsgCancelProposalResponse, error) {
	authorityAddr, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	proposal, err := k.VotingProposal.Get(ctx, msg.ProposalId)
	if err != nil {
		return nil, types.ErrProposalNotFound
	}

	// Auth check: proposer can cancel own; governance (module authority) can cancel any.
	isGov := bytes.Equal(authorityAddr, k.authority)
	isProposer := proposal.Proposer == msg.Authority
	if !isGov && !isProposer {
		return nil, types.ErrCancelNotAuthorized
	}

	// Status check: must be ACTIVE or TALLYING.
	if proposal.Status != types.ProposalStatus_PROPOSAL_STATUS_ACTIVE &&
		proposal.Status != types.ProposalStatus_PROPOSAL_STATUS_TALLYING {
		return nil, types.ErrProposalNotCancellable
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_CANCELLED
	proposal.FinalizedAt = sdkCtx.BlockHeight()

	if err := k.VotingProposal.Set(ctx, msg.ProposalId, proposal); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update proposal")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventProposalCancelled,
		sdk.NewAttribute(types.AttributeProposalID, fmt.Sprintf("%d", msg.ProposalId)),
		sdk.NewAttribute(types.AttributeReason, msg.Reason),
	))

	return &types.MsgCancelProposalResponse{}, nil
}
