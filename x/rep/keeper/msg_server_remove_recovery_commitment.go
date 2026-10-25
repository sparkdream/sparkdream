package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// RemoveRecoveryCommitment schedules a commitment's removal after
// recovery_change_delay; it stays usable for a reveal until then.
func (k msgServer) RemoveRecoveryCommitment(ctx context.Context, msg *types.MsgRemoveRecoveryCommitment) (*types.MsgRemoveRecoveryCommitmentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}
	if err := k.Keeper.RemoveRecoveryCommitment(ctx, msg.Member, msg.Commitment); err != nil {
		return nil, err
	}
	return &types.MsgRemoveRecoveryCommitmentResponse{}, nil
}
