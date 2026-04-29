package keeper

import (
	"context"
	"strings"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) SetPrimary(goCtx context.Context, msg *types.MsgSetPrimary) (*types.MsgSetPrimaryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Parse Authority Address
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
	}

	// 2. Normalize Name
	name := strings.ToLower(strings.TrimSpace(msg.Name))

	// 3. Verify Ownership
	// Users can only set a primary name if they currently own it.
	owner, found := k.GetNameOwner(ctx, name)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrNameNotFound, "name '%s' does not exist", name)
	}

	if owner.String() != msg.Authority {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "you do not own name '%s'", name)
	}

	// 4. Update OwnerInfo
	if err := k.SetPrimaryName(ctx, creatorAddr, name); err != nil {
		return nil, err
	}

	// Refresh owner activity so the owner's other names do not become scavengeable.
	if err := k.RecordOwnerActivity(ctx, msg.Authority); err != nil {
		return nil, err
	}

	return &types.MsgSetPrimaryResponse{}, nil
}
