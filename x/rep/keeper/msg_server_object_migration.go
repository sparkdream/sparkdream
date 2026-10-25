package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// ObjectMigration lets the old address contest a COUNCIL migration during
// its delay by posting migration_objection_bond; an x/rep jury decides.
func (k msgServer) ObjectMigration(ctx context.Context, msg *types.MsgObjectMigration) (*types.MsgObjectMigrationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}
	if err := k.Keeper.ObjectMigration(ctx, msg.Member, msg.MigrationId, msg.Reason); err != nil {
		return nil, err
	}
	return &types.MsgObjectMigrationResponse{}, nil
}
