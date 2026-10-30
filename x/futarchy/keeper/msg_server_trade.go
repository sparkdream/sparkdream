package keeper

import (
	"context"
	"fmt"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/futarchy/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) Trade(goCtx context.Context, msg *types.MsgTrade) (*types.MsgTradeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 0. Get params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 1. Fetch Market using Collections API
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

	// FUT-01: no trades at or after end_block. The market resolves at the
	// end of end_block on the time-weighted price, and a trade in that
	// block would otherwise run after the window closed.
	if ctx.BlockHeight() >= market.EndBlock {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "Market %d trading closed at block %d", msg.MarketId, market.EndBlock)
	}

	// 2. Setup Math Variables (No parsing needed)
	// BValue is *math.LegacyDec
	bValue := *market.BValue

	// PoolYes/PoolNo are *math.Int -> Convert to Dec for LMSR math
	poolYes := math.LegacyNewDecFromInt(*market.PoolYes)
	poolNo := math.LegacyNewDecFromInt(*market.PoolNo)

	// Construct Coin from the Int amount and Market Denom
	// msg.AmountIn is *math.Int, so we dereference it.
	if msg.AmountIn == nil || msg.AmountIn.IsNegative() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "invalid trade amount")
	}
	amountIn := sdk.NewCoin(market.Denom, *msg.AmountIn)

	// Validate denom matches market
	if amountIn.Denom != market.Denom {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "wrong denom: expected %s, got %s", market.Denom, amountIn.Denom)
	}

	// Check min_tick (Spam Protection)
	// MinTick is *math.Int
	minTick := math.LegacyNewDecFromInt(*market.MinTick)
	if math.LegacyNewDecFromInt(amountIn.Amount).LT(minTick) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "Trade too small, min tick is %s", minTick.String())
	}

	// 3. Calculate and deduct trading fee
	var feeAmount math.Int
	var amountAfterFee math.Int

	if params.TradingFeeBps > 0 {
		// Calculate fee: amount * bps / 10000
		feeDec := math.LegacyNewDecFromInt(amountIn.Amount).MulInt64(commontypes.Saturate[int64](params.TradingFeeBps)).QuoInt64(10000)
		feeAmount = feeDec.TruncateInt()
		amountAfterFee = amountIn.Amount.Sub(feeAmount)

		// Ensure we don't have negative amount after fee
		if amountAfterFee.LTE(math.ZeroInt()) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "trade amount too small after fee deduction")
		}
	} else {
		feeAmount = math.ZeroInt()
		amountAfterFee = amountIn.Amount
	}

	// 4. Accumulate the time-weighted price up to this block BEFORE the
	// pools move (FUT-01): the price set by the last trade of a block is the
	// price in effect for the following blocks.
	if err := k.accumulateTWAP(ctx, &market); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// 5. Solve for the new pool value (exact LMSR, no exponent clamp: FUT-03)
	maxExp, err := math.LegacyNewDecFromStr(params.MaxLmsrExponent)
	if err != nil {
		maxExp = types.DefaultMaxExponent
	}
	newPool, sharesOut, err := types.SharesForAmount(ctx, bValue, poolYes, poolNo, math.LegacyNewDecFromInt(amountAfterFee), msg.IsYes)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	if !sharesOut.IsPositive() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "trade amount too low to buy 1 share")
	}

	// Update Market State (Convert Dec back to Int pointer). Truncation
	// leaves the trader with slightly fewer shares, never more.
	val := newPool.TruncateInt()
	if msg.IsYes {
		market.PoolYes = &val
	} else {
		market.PoolNo = &val
	}

	// max_lmsr_exponent bounds how far the price may be pushed; a trade that
	// would cross it is rejected rather than mispriced (FUT-03).
	if err := types.CheckPriceBound(bValue, math.LegacyNewDecFromInt(*market.PoolYes), math.LegacyNewDecFromInt(*market.PoolNo), maxExp); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// 6. Transfer Funds (User -> Module)
	senderAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
	}

	err = k.bankKeeper.SendCoinsFromAccountToModule(goCtx, senderAddr, types.ModuleName, sdk.NewCoins(amountIn))
	if err != nil {
		return nil, err
	}

	// 7. Send fee to fee collector (if any fee)
	if feeAmount.GT(math.ZeroInt()) {
		feeCoin := sdk.NewCoin(amountIn.Denom, feeAmount)
		err = k.bankKeeper.SendCoinsFromModuleToModule(goCtx, types.ModuleName, "fee_collector", sdk.NewCoins(feeCoin))
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to send trading fee to fee_collector")
		}
	}

	// 8. Mint Conditional Tokens
	// Denom format: f/{marketId}/{outcome}
	outcomeStr := "no"
	if msg.IsYes {
		outcomeStr = "yes"
	}

	shareDenom := fmt.Sprintf("f/%d/%s", msg.MarketId, outcomeStr)

	// Handle Truncation: BigInt() truncates.
	sharesInt := sharesOut.TruncateInt()
	if sharesInt.IsZero() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "trade amount too low to buy 1 share")
	}
	// FUT-06: optional slippage bound.
	if msg.MinSharesOut != nil && !msg.MinSharesOut.IsNil() && sharesInt.LT(*msg.MinSharesOut) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "slippage: %s shares out < min_shares_out %s", sharesInt.String(), msg.MinSharesOut.String())
	}

	sharesCoin := sdk.NewCoin(shareDenom, sharesInt)

	// Mint and Send
	err = k.bankKeeper.MintCoins(goCtx, types.ModuleName, sdk.NewCoins(sharesCoin))
	if err != nil {
		return nil, err
	}

	err = k.bankKeeper.SendCoinsFromModuleToAccount(goCtx, types.ModuleName, senderAddr, sdk.NewCoins(sharesCoin))
	if err != nil {
		return nil, err
	}

	// 9. Save Market using Collections API
	err = k.Market.Set(ctx, msg.MarketId, market)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"market_trade",
			sdk.NewAttribute("market_id", fmt.Sprintf("%d", msg.MarketId)),
			sdk.NewAttribute("trader", msg.Creator),
			sdk.NewAttribute("outcome", outcomeStr),
			sdk.NewAttribute("amount_in", amountIn.String()),
			sdk.NewAttribute("fee", feeAmount.String()),
			sdk.NewAttribute("shares_out", sharesInt.String()),
			sdk.NewAttribute("pool_yes", market.PoolYes.String()),
			sdk.NewAttribute("pool_no", market.PoolNo.String()),
		),
	)

	return &types.MsgTradeResponse{
		SharesOut: &sharesInt,
	}, nil
}
