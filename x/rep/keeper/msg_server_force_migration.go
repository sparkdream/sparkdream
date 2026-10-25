package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// ForceMigration opens a COUNCIL-path migration on behalf of the Commons HR
// committee policy or governance, for a member who lost their key without a
// recovery commitment. The new address must accept; the old address may
// object to a jury during the delay.
func (k msgServer) ForceMigration(ctx context.Context, msg *types.MsgForceMigration) (*types.MsgForceMigrationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.NewAddress); err != nil {
		return nil, errorsmod.Wrap(err, "invalid new address")
	}
	id, err := k.Keeper.ForceMigration(ctx, msg.Authority, msg.Member, msg.NewAddress, msg.EvidenceUri)
	if err != nil {
		return nil, err
	}
	return &types.MsgForceMigrationResponse{MigrationId: id}, nil
}
