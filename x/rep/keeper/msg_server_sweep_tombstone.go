package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// SweepTombstone moves a tombstone's spendable SPARK (unbonded stake that
// landed there after finalization) to its canonical address, which signs.
func (k msgServer) SweepTombstone(ctx context.Context, msg *types.MsgSweepTombstone) (*types.MsgSweepTombstoneResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Signer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid signer address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Tombstone); err != nil {
		return nil, errorsmod.Wrap(err, "invalid tombstone address")
	}
	if _, err := k.Keeper.SweepTombstone(ctx, msg.Signer, msg.Tombstone); err != nil {
		return nil, err
	}
	return &types.MsgSweepTombstoneResponse{}, nil
}
