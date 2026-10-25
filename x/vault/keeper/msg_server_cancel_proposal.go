package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/vault/types"
)

func (k msgServer) CancelProposal(ctx context.Context, msg *types.MsgCancelProposal) (*types.MsgCancelProposalResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Signer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid signer address")
	}
	if err := k.Keeper.Cancel(ctx, msg.Signer, msg.ProposalId); err != nil {
		return nil, err
	}
	return &types.MsgCancelProposalResponse{}, nil
}
