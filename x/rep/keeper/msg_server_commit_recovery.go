package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// CommitRecovery stores a sealed commit (the reveal's front-running guard)
// against recovery_commit_deposit. Any account may submit it; it reveals
// nothing about the member, the preimage or the target.
func (k msgServer) CommitRecovery(ctx context.Context, msg *types.MsgCommitRecovery) (*types.MsgCommitRecoveryResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid submitter address")
	}
	if err := k.Keeper.CommitRecovery(ctx, msg.Submitter, msg.Sealed); err != nil {
		return nil, err
	}
	return &types.MsgCommitRecoveryResponse{}, nil
}
