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

	// Parse market state
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
	liquidityWithdrawn, ok := math.NewIntFromString(market.LiquidityWithdrawn)
	if !ok {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "Invalid LiquidityWithdrawn in state")
	}

	// Calculate total shares minted
	totalShares := poolYes.Add(poolNo)
	totalSharesInt := totalShares.TruncateInt()

	// Calculate available liquidity
	// Formula: initial_liquidity - total_shares_minted - already_withdrawn
	availableLiquidity := initialLiquidity.Sub(totalSharesInt).Sub(liquidityWithdrawn)

	if availableLiquidity.LTE(math.ZeroInt()) {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "No liquidity available to withdraw")
	}

	// Update market state
	newWithdrawn := liquidityWithdrawn.Add(availableLiquidity)
	market.LiquidityWithdrawn = newWithdrawn.String()

	if err := k.Market.Set(ctx, msg.MarketId, market); err != nil {
		return nil, err
	}

	// Transfer liquidity to creator
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
	}

	withdrawCoin := sdk.NewCoin(market.Denom, availableLiquidity)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(goCtx, types.ModuleName, creatorAddr, sdk.NewCoins(withdrawCoin)); err != nil {
		return nil, err
	}

	// Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"liquidity_withdrawn",
			sdk.NewAttribute("market_id", fmt.Sprintf("%d", msg.MarketId)),
			sdk.NewAttribute("creator", msg.Creator),
			sdk.NewAttribute("amount", availableLiquidity.String()),
		),
	)

	return &types.MsgWithdrawLiquidityResponse{
		Amount: availableLiquidity.String(),
	}, nil
}
