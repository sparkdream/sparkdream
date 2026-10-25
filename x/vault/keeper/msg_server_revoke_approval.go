package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/vault/types"
)

func (k msgServer) RevokeApproval(ctx context.Context, msg *types.MsgRevokeApproval) (*types.MsgRevokeApprovalResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Signer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid signer address")
	}
	if err := k.Keeper.RevokeApproval(ctx, msg.Signer, msg.ProposalId); err != nil {
		return nil, err
	}
	return &types.MsgRevokeApprovalResponse{}, nil
}
