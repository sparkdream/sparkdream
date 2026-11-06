package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AbandonInterim(ctx context.Context, msg *types.MsgAbandonInterim) (*types.MsgAbandonInterimResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get interim
	interim, err := k.GetInterim(ctx, msg.InterimId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get interim")
	}

	// Validate creator is an assignee
	found := false
	for _, assignee := range interim.Assignees {
		if k.Keeper.CanActAs(ctx, msg.Creator, assignee) {
			found = true
			break
		}
	}
	if !found {
		return nil, errorsmod.Wrap(types.ErrNotAssignee, "only assignee can abandon interim")
	}

	// Only live work can be abandoned (REP-E-13): relabelling a COMPLETED,
	// already paid interim as EXPIRED and overwriting its notes rewrote the
	// audit trail. An ADJUDICATION interim belongs to the committee, not to
	// the authority address listed as its assignee.
	if interim.Status != types.InterimStatus_INTERIM_STATUS_PENDING &&
		interim.Status != types.InterimStatus_INTERIM_STATUS_IN_PROGRESS {
		return nil, errorsmod.Wrapf(types.ErrInvalidInterimStatus,
			"interim %d is %s and can no longer be abandoned", interim.Id, interim.Status)
	}
	if interim.Type == types.InterimType_INTERIM_TYPE_ADJUDICATION {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "an adjudication interim cannot be abandoned")
	}

	// A project review is the committee's seat, not one member's work: an
	// assignee who abandons it gives up only their own seat (and its share),
	// the review stays live for the rest and still settles when the project
	// is approved (REP-E-206). With the last seat gone it expires unpaid.
	if IsProjectReviewInterimType(interim.Type) {
		var own []string
		for _, assignee := range interim.Assignees {
			if k.Keeper.CanActAs(ctx, msg.Creator, assignee) {
				own = append(own, assignee)
			}
		}
		if err := k.Keeper.dropInterimAssignees(ctx, interim, own, "abandoned: "+msg.Reason); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update interim")
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
			"interim_abandoned",
			sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interim.Id)),
			sdk.NewAttribute("assignee", msg.Creator),
			sdk.NewAttribute("reason", msg.Reason),
		))
		return &types.MsgAbandonInterimResponse{}, nil
	}

	// Mark interim as abandoned/expired
	interim.Status = types.InterimStatus_INTERIM_STATUS_EXPIRED
	interim.CompletionNotes = msg.Reason

	if err := k.UpdateInterim(ctx, interim); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update interim")
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"interim_abandoned",
		sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interim.Id)),
		sdk.NewAttribute("assignee", msg.Creator),
		sdk.NewAttribute("reason", msg.Reason),
	))

	return &types.MsgAbandonInterimResponse{}, nil
}
