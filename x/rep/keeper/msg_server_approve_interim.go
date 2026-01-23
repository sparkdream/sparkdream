package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) ApproveInterim(ctx context.Context, msg *types.MsgApproveInterim) (*types.MsgApproveInterimResponse, error) {
	approverAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid approver address")
	}

	// Approve the interim using the keeper method
	if err := k.Keeper.ApproveInterim(ctx, msg.InterimId, approverAddr, msg.Approved, msg.Comments); err != nil {
		return nil, errorsmod.Wrap(err, "failed to approve interim")
	}

	return &types.MsgApproveInterimResponse{}, nil
}
