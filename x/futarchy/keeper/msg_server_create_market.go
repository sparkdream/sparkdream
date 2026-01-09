package keeper

import (
	"context"

	"sparkdream/x/futarchy/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) CreateMarket(goCtx context.Context, msg *types.MsgCreateMarket) (*types.MsgCreateMarketResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, err
	}

	// Calculate duration (EndBlock - Current)
	duration := msg.EndBlock - ctx.BlockHeight()
	if duration <= 0 {
		return nil, types.ErrInvalidDuration
	}

	// Validate InitialLiquidity exists
	if msg.InitialLiquidity == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "initial liquidity cannot be nil")
	}

	// Explicitly check for negative values
	if msg.InitialLiquidity.IsNegative() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "initial liquidity cannot be negative")
	}

	// Standard users get 0 delay (Liquid Markets)
	liquidity := sdk.NewCoin("uspark", *msg.InitialLiquidity)

	// Validate the resulting coin object (checks for valid denom and non-negative amount)
	if !liquidity.IsValid() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "initial liquidity coin is invalid")
	}

	id, err := k.CreateMarketInternal(ctx, creator, msg.Symbol, msg.Question, duration, 0, liquidity)
	if err != nil {
		return nil, err
	}

	return &types.MsgCreateMarketResponse{MarketId: id}, nil
}
