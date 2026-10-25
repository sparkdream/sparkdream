package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// AcceptMigration is signed by the new address and completes every path: it
// proves control of the target (so nobody migrates into a typo), starts the
// path's delay and enqueues finalization. A native multisig target
// multisigns this message.
func (k msgServer) AcceptMigration(ctx context.Context, msg *types.MsgAcceptMigration) (*types.MsgAcceptMigrationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.NewAddress); err != nil {
		return nil, errorsmod.Wrap(err, "invalid new address")
	}
	if err := k.Keeper.AcceptMigration(ctx, msg.NewAddress, msg.MigrationId); err != nil {
		return nil, err
	}
	return &types.MsgAcceptMigrationResponse{}, nil
}
