package keeper

import (
	"context"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) UpdateName(goCtx context.Context, msg *types.MsgUpdateName) (*types.MsgUpdateNameResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Retrieve the existing name record
	// We rely on the Keeper to fetch the record directly.
	val, err := k.Names.Get(ctx, msg.Name)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrNameNotFound, "name does not exist")
	}

	// 2. Check Ownership
	// Only the current owner can update the metadata.
	if msg.Creator != val.Owner {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "incorrect owner")
	}

	// 3. Update the Metadata
	val.Data = msg.Data

	// 4. Save the updated record
	if err := k.Names.Set(ctx, msg.Name, val); err != nil {
		return nil, err
	}

	// 5. Emit an event (Optional but recommended)
	ctx.EventManager().EmitEvent(
		sdk.NewEvent("name_updated",
			sdk.NewAttribute("name", msg.Name),
			sdk.NewAttribute("owner", msg.Creator),
			sdk.NewAttribute("new_data", msg.Data),
		),
	)

	return &types.MsgUpdateNameResponse{
		Name:  msg.Name,
		Owner: val.Owner,
	}, nil
}
