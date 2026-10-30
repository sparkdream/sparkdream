package keeper

import (
	"context"

	"sparkdream/x/vault/types"

	errorsmod "cosmossdk.io/errors"
)

// AcceptVaultSigner accepts the signer's own (alias-aware) unaccepted entry.
func (k msgServer) AcceptVaultSigner(ctx context.Context, msg *types.MsgAcceptVaultSigner) (*types.MsgAcceptVaultSignerResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Signer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid signer address")
	}
	if err := k.Keeper.AcceptSigner(ctx, msg.Signer, msg.VaultId); err != nil {
		return nil, err
	}
	return &types.MsgAcceptVaultSignerResponse{}, nil
}
