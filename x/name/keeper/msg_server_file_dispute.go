package keeper

import (
	"context"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) FileDispute(goCtx context.Context, msg *types.MsgFileDispute) (*types.MsgFileDisputeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)

	// 1. Get Council Address using our helper (replaces the undefined groupKeeper call)
	councilAddr, err := k.GetCouncilAddress(ctx, params.CouncilGroupId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to resolve council address")
	}

	// 2. Deduct Dispute Fee (Transfer from User to Treasury/Council)
	claimant, _ := sdk.AccAddressFromBech32(msg.Authority)

	if !params.DisputeFee.IsZero() {
		// Send directly to the Council's Policy Account
		err := k.bankKeeper.SendCoins(ctx, claimant, councilAddr, sdk.NewCoins(params.DisputeFee))
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
