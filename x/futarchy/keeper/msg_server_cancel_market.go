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

	market, err := k.Market.Get(ctx, msg.MarketId)
	if err != nil {
		if errorsmod.IsOf(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "Market %d not found", msg.MarketId)
		}
		return nil, err
	}

	if market.Status != "ACTIVE" {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "Market %d is not active (status: %s)", msg.MarketId, market.Status)
	}

	if market.BValue == nil || market.PoolYes == nil || market.PoolNo == nil ||
		market.InitialLiquidity == nil || market.LiquidityWithdrawn == nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "Market %d missing LMSR fields", msg.MarketId)
	}

	market.Status = "CANCELLED"
	market.ResolutionHeight = ctx.BlockHeight()

	if err := k.ActiveMarkets.Remove(ctx, collections.Join(market.EndBlock, msg.MarketId)); err != nil {
		ctx.Logger().Info("market not in active index during cancellation", "market_id", msg.MarketId)
	}

	// FUTARCHY-S2-1 (trapped funds): if any trades happened, snapshot the
	// LMSR-implied YES price on the market. Holders later use Redeem on the
	// CANCELLED market to claim their pro-rata share of the collateral; the
	// creator's withdraw is the LMSR-residual (entropy-bounded subsidy).
	hasTrades := market.PoolYes.IsPositive() || market.PoolNo.IsPositive()
	if hasTrades {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return nil, err
		}
		maxExp, mErr := math.LegacyNewDecFromStr(params.MaxLmsrExponent)
		if mErr != nil {
			maxExp = types.DefaultMaxExponent
		}
		qYes := math.LegacyNewDecFromInt(*market.PoolYes)
		qNo := math.LegacyNewDecFromInt(*market.PoolNo)
		pYes, pErr := types.SettlementPriceYes(ctx, *market.BValue, qYes, qNo, maxExp)
		if pErr != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, pErr.Error())
		}
		market.SettlementPriceYes = &pYes
	}

	residual, err := k.computeCreatorResidual(ctx, market)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	if residual.GT(*market.InitialLiquidity) {
		residual = *market.InitialLiquidity
	}

	alreadyWithdrawn := *market.LiquidityWithdrawn
	liquidityToRefund := residual.Sub(alreadyWithdrawn)
	if liquidityToRefund.IsNegative() {
		liquidityToRefund = math.ZeroInt()
	}

	if liquidityToRefund.IsPositive() {
		creatorAddr, err := sdk.AccAddressFromBech32(market.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
		}
		refundCoin := sdk.NewCoin(market.Denom, liquidityToRefund)
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(goCtx, types.ModuleName, creatorAddr, sdk.NewCoins(refundCoin)); err != nil {
			return nil, err
		}
		newWithdrawn := alreadyWithdrawn.Add(liquidityToRefund)
		market.LiquidityWithdrawn = &newWithdrawn
	}

	if err := k.Market.Set(ctx, msg.MarketId, market); err != nil {
		return nil, err
	}

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
