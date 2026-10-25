package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// CancelMigration cancels a pending migration. The old or new address may
// cancel a KEY migration; the Commons HR committee policy may cancel KEY and
// COUNCIL migrations; governance may cancel any path and is the only
// authority over a RECOVERY migration.
func (k msgServer) CancelMigration(ctx context.Context, msg *types.MsgCancelMigration) (*types.MsgCancelMigrationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Signer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid signer address")
	}
	if err := k.Keeper.CancelMigration(ctx, msg.Signer, msg.MigrationId); err != nil {
		return nil, err
	}
	return &types.MsgCancelMigrationResponse{}, nil
}
