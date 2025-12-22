package keeper

import (
	"context"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) FileDispute(goCtx context.Context, msg *types.MsgFileDispute) (*types.MsgFileDisputeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)

	// 1. Get Council Address using k.commonsKeeper.GetExtendedGroup
	// We assume the Council name is "Commons Council" and its PolicyAddress is the recipient.
	councilGroup, err := k.commonsKeeper.GetExtendedGroup(ctx, "Commons Council")
	if err != nil {
		// Return a specific error if the required governance group isn't found
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to resolve required council address: %s", err.Error())
	}

	// The recipient of the dispute fee is the Council's Policy Address.
	councilPolicyAddr, err := sdk.AccAddressFromBech32(councilGroup.PolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "invalid policy address stored for Commons Council: %s", councilGroup.PolicyAddress)
	}

	// 2. Deduct Dispute Fee (Transfer from User to Treasury/Council)
	claimant, _ := sdk.AccAddressFromBech32(msg.Authority)

	if !params.DisputeFee.IsZero() {
		// Send directly to the Council's Policy Account
		err := k.bankKeeper.SendCoins(ctx, claimant, councilPolicyAddr, sdk.NewCoins(params.DisputeFee))
		if err != nil {
			return nil, errorsmod.Wrap(err, "insufficient funds for dispute fee")
		}
	}

	// 3. Create Dispute Record
	// We store the dispute keyed by the Name being contested
	dispute := types.Dispute{
		Name:     msg.Name,
		Claimant: msg.Authority,
	}

	// Use the helper from keeper_helpers.go
	if err := k.SetDispute(ctx, dispute); err != nil {
		return nil, err
	}

	return &types.MsgFileDisputeResponse{}, nil
}
