package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) CompleteInterim(ctx context.Context, msg *types.MsgCompleteInterim) (*types.MsgCompleteInterimResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get the interim to check authorization
	interim, err := k.Keeper.GetInterim(ctx, msg.InterimId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "interim not found")
	}

	// Authorization check based on interim type
	if interim.Type == types.InterimType_INTERIM_TYPE_ADJUDICATION {
		// ADJUDICATION interims can only be completed by Technical Committee members
		if !k.Keeper.IsOperationsCommittee(ctx, creatorAddr) {
			return nil, errorsmod.Wrap(types.ErrUnauthorized,
				fmt.Sprintf("only technical committee members can complete ADJUDICATION interims (creator: %s)", msg.Creator))
		}
	} else {
		// Regular interims can only be completed by assignees
		isAssignee := false
		for _, assigneeStr := range interim.Assignees {
			if assigneeStr == msg.Creator {
				isAssignee = true
				break
			}
		}
		if !isAssignee {
			return nil, errorsmod.Wrap(types.ErrUnauthorized,
				fmt.Sprintf("only assignees can complete this interim (creator: %s not in assignees)", msg.Creator))
		}
	}

	// Complete the interim directly using the keeper method
	if err := k.Keeper.CompleteInterimDirectly(ctx, msg.InterimId, msg.CompletionNotes); err != nil {
		return nil, errorsmod.Wrap(err, "failed to complete interim")
	}

	return &types.MsgCompleteInterimResponse{}, nil
}
