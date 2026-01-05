package keeper

import (
	"context"

	"sparkdream/x/futarchy/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CreateMarket(goCtx context.Context, msg *types.MsgCreateMarket) (*types.MsgCreateMarketResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	liquidity, err := sdk.ParseCoinNormalized(msg.InitialLiquidity)
	if err != nil {
		return nil, err
	}

	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, err
	}

	// Calculate duration (EndBlock - Current)
	duration := msg.EndBlock - ctx.BlockHeight()
	if duration <= 0 {
		return nil, types.ErrInvalidDuration
	}

	// Standard users get 0 delay (Liquid Markets)
	id, err := k.CreateMarketInternal(ctx, creator, msg.Symbol, msg.Question, duration, 0, liquidity)
	if err != nil {
		return nil, err
	}

	return &types.MsgCreateMarketResponse{MarketId: id}, nil
}
