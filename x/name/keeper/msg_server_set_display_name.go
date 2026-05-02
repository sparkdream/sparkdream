package keeper

import (
	"context"
	"errors"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) SetDisplayName(goCtx context.Context, msg *types.MsgSetDisplayName) (*types.MsgSetDisplayNameResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid authority address")
	}

	if err := validateDisplayName(msg.DisplayName); err != nil {
		return nil, err
	}

	info, err := k.Owners.Get(ctx, msg.Authority)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return nil, err
		}
		info = types.OwnerInfo{Address: msg.Authority}
	}

	info.DisplayName = msg.DisplayName
	info.LastActiveTime = ctx.BlockTime().Unix()

	if err := k.Owners.Set(ctx, msg.Authority, info); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent("display_name_set",
			sdk.NewAttribute("address", msg.Authority),
			sdk.NewAttribute("display_name", msg.DisplayName),
		),
	)

	return &types.MsgSetDisplayNameResponse{}, nil
}
