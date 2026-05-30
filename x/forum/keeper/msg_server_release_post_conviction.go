package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

// ReleasePostConviction closes a PostConvictionStake after the lock window
// has passed and unlocks the staker's remaining DREAM. Slashed stakes
// (from ExpireHiddenPosts) are already released; calling here on a
// released stake is rejected by the keeper.
func (k msgServer) ReleasePostConviction(
	ctx context.Context,
	msg *types.MsgReleasePostConviction,
) (*types.MsgReleasePostConvictionResponse, error) {
	callerBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	caller := sdk.AccAddress(callerBytes)

	if err := k.Keeper.ReleasePostConvictionStake(ctx, msg.StakeId, caller); err != nil {
		return nil, err
	}
	return &types.MsgReleasePostConvictionResponse{}, nil
}
