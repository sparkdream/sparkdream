package keeper

import (
	"context"

	"sparkdream/x/vault/types"

	errorsmod "cosmossdk.io/errors"
)

// LeaveVault removes the signer's own (alias-aware) entry from a vault, or
// declines an unaccepted listing.
func (k msgServer) LeaveVault(ctx context.Context, msg *types.MsgLeaveVault) (*types.MsgLeaveVaultResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Signer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid signer address")
	}
	if err := k.Keeper.LeaveVault(ctx, msg.Signer, msg.VaultId); err != nil {
		return nil, err
	}
	return &types.MsgLeaveVaultResponse{}, nil
}
