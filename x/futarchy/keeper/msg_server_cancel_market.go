package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/futarchy/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) CancelMarket(goCtx context.Context, msg *types.MsgCancelMarket) (*types.MsgCancelMarketResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Verify authority
	authorityStr, err := k.addressCodec.BytesToString(k.authority)
	if err != nil {
		return nil, err
	}
	if authorityStr != msg.Authority {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority; expected %s, got %s", authorityStr, msg.Authority)
	}

	// Fetch market
	market, err := k.Market.Get(ctx, msg.MarketId)
	if err != nil {
		if errorsmod.IsOf(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "Market %d not found", msg.MarketId)
		}
		return nil, err
	}

	// Only active markets can be cancelled
	if market.Status != "ACTIVE" {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "Market %d is not active (status: %s)", msg.MarketId, market.Status)
	}

	// Mark market as CANCELLED
	market.Status = "CANCELLED"
	market.ResolutionHeight = ctx.BlockHeight()

	// Save updated market
	if err := k.Market.Set(ctx, msg.MarketId, market); err != nil {
		return nil, err
	}

	// Remove from active markets index
	if err := k.ActiveMarkets.Remove(ctx, collections.Join(market.EndBlock, msg.MarketId)); err != nil {
		// Log but don't fail if not in index
		ctx.Logger().Info("market not in active index during cancellation", "market_id", msg.MarketId)
	}

	// Refund remaining liquidity to creator
	// Calculate how much liquidity is left in the market
	poolYes, err := math.LegacyNewDecFromStr(market.PoolYes)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "Invalid PoolYes in state")
	}
	poolNo, err := math.LegacyNewDecFromStr(market.PoolNo)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "Invalid PoolNo in state")
	}
	initialLiquidity, ok := math.NewIntFromString(market.InitialLiquidity)
	if !ok {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "Invalid InitialLiquidity in state")
	}

	// Total shares minted = poolYes + poolNo
	totalShares := poolYes.Add(poolNo)
	totalSharesInt := totalShares.TruncateInt()

	// Liquidity to refund = initial_liquidity - total_shares_minted
	// (since each share minted required 1 unit of liquidity from users)
	liquidityToRefund := initialLiquidity.Sub(totalSharesInt)

	if liquidityToRefund.IsPositive() {
		creatorAddr, err := sdk.AccAddressFromBech32(market.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
		}

		refundCoin := sdk.NewCoin(market.Denom, liquidityToRefund)
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(goCtx, types.ModuleName, creatorAddr, sdk.NewCoins(refundCoin)); err != nil {
			return nil, err
		}
	}

	// Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"market_cancelled",
			sdk.NewAttribute("market_id", fmt.Sprintf("%d", msg.MarketId)),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("refunded", liquidityToRefund.String()),
		),
	)

	return &types.MsgCancelMarketResponse{}, nil
}
