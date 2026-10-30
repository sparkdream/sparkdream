package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// RevealRecovery opens a RECOVERY migration by revealing the preimage of
// one of the member's active commitments. Signed by any account (the
// revealer); the sealed commit already binds the target, and the target
// confirms control afterwards with MsgAcceptMigration.
func (k msgServer) RevealRecovery(ctx context.Context, msg *types.MsgRevealRecovery) (*types.MsgRevealRecoveryResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Revealer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid revealer address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.NewAddress); err != nil {
		return nil, errorsmod.Wrap(err, "invalid new address")
	}
	id, reason, err := k.Keeper.RevealRecovery(ctx, msg.Revealer, msg.Member, msg.NewAddress, msg.Preimage)
	if err != nil {
		return nil, err
	}
	return &types.MsgRevealRecoveryResponse{MigrationId: id, FailureReason: reason}, nil
}
