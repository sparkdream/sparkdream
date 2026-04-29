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

func (k msgServer) WithdrawLiquidity(goCtx context.Context, msg *types.MsgWithdrawLiquidity) (*types.MsgWithdrawLiquidityResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Fetch market
	market, err := k.Market.Get(ctx, msg.MarketId)
	if err != nil {
		if errorsmod.IsOf(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "Market %d not found", msg.MarketId)
		}
		return nil, err
	}

	// Only market creator can withdraw liquidity
	if market.Creator != msg.Creator {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "only market creator can withdraw liquidity")
	}

	// Market must be resolved
	if market.Status != "RESOLVED_YES" && market.Status != "RESOLVED_NO" && market.Status != "RESOLVED_INVALID" {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "Market must be resolved before withdrawing liquidity (current status: %s)", market.Status)
	}

	initialLiquidity := *market.InitialLiquidity
	liquidityWithdrawn := *market.LiquidityWithdrawn

	// FUTARCHY-S2-1: refund the LMSR-correct creator residual, not the full
	// InitialLiquidity. The wrong formula drained shared module collateral
	// and caused redemptions to fail when trades pushed the market to a
	// corner. See types/lmsr.go for the residual derivation.
	residual, err := k.computeCreatorResidual(ctx, market)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	// Cap at InitialLiquidity defensively; the math should never exceed it
	// but rounding could otherwise let the creator over-withdraw.
	if residual.GT(initialLiquidity) {
		residual = initialLiquidity
	}

	availableLiquidity := residual.Sub(liquidityWithdrawn)
	if availableLiquidity.LTE(math.ZeroInt()) {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "No liquidity available to withdraw")
	}

	newWithdrawn := liquidityWithdrawn.Add(availableLiquidity)
	market.LiquidityWithdrawn = &newWithdrawn

	if err := k.Market.Set(ctx, msg.MarketId, market); err != nil {
		return nil, err
	}

	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
	}

	withdrawCoin := sdk.NewCoin(market.Denom, availableLiquidity)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(goCtx, types.ModuleName, creatorAddr, sdk.NewCoins(withdrawCoin)); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"liquidity_withdrawn",
			sdk.NewAttribute("market_id", fmt.Sprintf("%d", msg.MarketId)),
			sdk.NewAttribute("creator", msg.Creator),
			sdk.NewAttribute("amount", availableLiquidity.String()),
		),
	)

	return &types.MsgWithdrawLiquidityResponse{
		Amount: &availableLiquidity,
	}, nil
}
