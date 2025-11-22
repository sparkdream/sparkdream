package keeper

import (
	"context"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) ResolveDispute(goCtx context.Context, msg *types.MsgResolveDispute) (*types.MsgResolveDisputeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)

	// 1. Authority Check (Must be Council)
	councilAddr, err := k.GetCouncilAddress(ctx, params.CouncilGroupId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "council address not found")
	}

	// Note: msg.Authority is the signer. For a group proposal, the signer is the Group Policy Address.
	if msg.Authority != councilAddr.String() {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "only council policy %s can resolve disputes", councilAddr)
	}

	// 2. Verify Payment Proof (Does the dispute exist?)
	_, found := k.GetDispute(ctx, msg.Name)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrDisputeNotFound, "no active dispute found for %s; fee has not been paid", msg.Name)
	}

	// 3. Execute the Transfer
	// A. Remove from old owner
	currentOwner, found := k.GetNameOwner(ctx, msg.Name)
	if found {
		k.RemoveNameFromOwner(ctx, currentOwner, msg.Name)
	}

	// B. Add to new owner
	newOwner, _ := sdk.AccAddressFromBech32(msg.NewOwner)
	k.AddNameToOwner(ctx, newOwner, msg.Name)

	// C. Update Record
	record, found := k.GetName(ctx, msg.Name)
	if !found {
		// Ensure we have a valid record structure even if it didn't exist before
		record = types.NameRecord{Name: msg.Name}
	}
	record.Owner = msg.NewOwner
	k.SetName(ctx, record)

	// 4. Close the Dispute (Delete the ticket so it can't be used twice)
	k.RemoveDispute(ctx, msg.Name)

	return &types.MsgResolveDisputeResponse{}, nil
}
