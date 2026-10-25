package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/vault/types"
)

func (k msgServer) Approve(ctx context.Context, msg *types.MsgApprove) (*types.MsgApproveResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Signer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid signer address")
	}
	v, p, err := k.Keeper.Approve(ctx, msg.Signer, msg.ProposalId, msg.MessagesHash)
	if err != nil {
		return nil, err
	}
	executed := false
	if msg.Execute && k.Tally(ctx, v, p) >= v.Threshold {
		if err := k.Keeper.Execute(ctx, msg.Signer, msg.ProposalId); err != nil {
			return nil, err
		}
		executed = true
	}
	return &types.MsgApproveResponse{Executed: executed}, nil
}
