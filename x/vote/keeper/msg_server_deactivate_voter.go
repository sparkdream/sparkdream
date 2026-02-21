package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) DeactivateVoter(ctx context.Context, msg *types.MsgDeactivateVoter) (*types.MsgDeactivateVoterResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Voter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid voter address")
	}

	reg, err := k.VoterRegistration.Get(ctx, msg.Voter)
	if err != nil {
		return nil, types.ErrNotRegistered
	}

	if !reg.Active {
		return nil, types.ErrAlreadyInactive
	}

	reg.Active = false
	if err := k.VoterRegistration.Set(ctx, msg.Voter, reg); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update voter registration")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventVoterDeactivated,
		sdk.NewAttribute(types.AttributeVoter, msg.Voter),
		sdk.NewAttribute(types.AttributeReason, "voluntary"),
	))

	return &types.MsgDeactivateVoterResponse{}, nil
}
