package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// SetRecoveryCommitment adds (or replaces) a recovery commitment. It takes
// effect after recovery_change_delay: a thief holding the key cannot swap in
// their own commitment, and the recovery path cannot be used as a faster key
// path.
func (k msgServer) SetRecoveryCommitment(ctx context.Context, msg *types.MsgSetRecoveryCommitment) (*types.MsgSetRecoveryCommitmentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}
	if err := k.Keeper.SetRecoveryCommitment(ctx, msg.Member, msg.Commitment, msg.Replaces, false); err != nil {
		return nil, err
	}
	return &types.MsgSetRecoveryCommitmentResponse{}, nil
}
