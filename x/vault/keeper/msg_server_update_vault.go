package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/vault/types"
)

// UpdateVault's signer is the vault address, for which no key exists, so the
// only way here is an executed proposal of that vault.
func (k msgServer) UpdateVault(ctx context.Context, msg *types.MsgUpdateVault) (*types.MsgUpdateVaultResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Vault); err != nil {
		return nil, errorsmod.Wrap(err, "invalid vault address")
	}
	if err := k.Keeper.UpdateVault(ctx, msg.Vault, msg.Signers, msg.Threshold, msg.ProposalTtl, msg.Label); err != nil {
		return nil, err
	}
	return &types.MsgUpdateVaultResponse{}, nil
}
