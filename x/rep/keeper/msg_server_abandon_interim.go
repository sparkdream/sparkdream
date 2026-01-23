package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
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
		if assignee == msg.Creator {
			found = true
			break
		}
	}
	if !found {
		return nil, errorsmod.Wrap(types.ErrNotAssignee, "only assignee can abandon interim")
	}

	// Mark interim as abandoned/expired
	interim.Status = types.InterimStatus_INTERIM_STATUS_EXPIRED
	interim.CompletionNotes = msg.Reason

	if err := k.UpdateInterim(ctx, interim); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update interim")
	}

	return &types.MsgAbandonInterimResponse{}, nil
}
