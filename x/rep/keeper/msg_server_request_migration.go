package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// RequestMigration opens a KEY-path migration of the signer's identity to a
// fresh address. No fee: safety actions pay gas only. The new address must
// accept with MsgAcceptMigration within migration_accept_window.
func (k msgServer) RequestMigration(ctx context.Context, msg *types.MsgRequestMigration) (*types.MsgRequestMigrationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.NewAddress); err != nil {
		return nil, errorsmod.Wrap(err, "invalid new address")
	}
	id, err := k.RequestKeyMigration(ctx, msg.Member, msg.NewAddress)
	if err != nil {
		return nil, err
	}
	return &types.MsgRequestMigrationResponse{MigrationId: id}, nil
}
