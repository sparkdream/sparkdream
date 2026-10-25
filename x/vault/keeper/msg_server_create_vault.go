package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/vault/types"
)

func (k msgServer) CreateVault(ctx context.Context, msg *types.MsgCreateVault) (*types.MsgCreateVaultResponse, error) {
	creator, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	v, err := k.Keeper.CreateVault(ctx, creator, msg.Signers, msg.Threshold, msg.ProposalTtl, msg.Label)
	if err != nil {
		return nil, err
	}
	return &types.MsgCreateVaultResponse{VaultId: v.VaultId, Address: v.Address}, nil
}
