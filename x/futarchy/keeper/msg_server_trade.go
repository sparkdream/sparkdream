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
		feeDec := math.LegacyNewDecFromInt(amountIn.Amount).MulInt64(int64(params.TradingFeeBps)).QuoInt64(10000)
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

	// 4. Calculate "Current Cost" (Pass ctx)
	currentCost, err := types.CalculateLMSRCost(ctx, bValue, poolYes, poolNo)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// 5. Calculate "New Cost" using amount after fee
	newCost := currentCost.Add(math.LegacyNewDecFromInt(amountAfterFee))

	var newPoolYes, newPoolNo math.LegacyDec
	var sharesOut math.LegacyDec

	if msg.IsYes {
		// Solve for newYes: q1 = C + b * ln(1 - e^((q2 - C)/b))
		// Use stable formula to avoid overflow in Exp(C/b)
		exponent := poolNo.Sub(newCost).Quo(bValue)
		// Clamp exponent for numerical stability
		exponent = types.ClampExponent(exponent, types.DefaultMaxExponent)
		expTerm := types.Exp(ctx, exponent)
		oneMinus := math.LegacyOneDec().Sub(expTerm)

		if oneMinus.LTE(math.LegacyZeroDec()) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest,
				"trade too large: would deplete market liquidity")
		}

		lnTerm, err := types.Ln(ctx, oneMinus)
		if err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
		}
		newPoolYes = newCost.Add(bValue.Mul(lnTerm))
		sharesOut = newPoolYes.Sub(poolYes)

		// Update Market State (Convert Dec back to Int pointer)
		val := newPoolYes.TruncateInt()
		market.PoolYes = &val
	} else {
		// Solve for newNo
		exponent := poolYes.Sub(newCost).Quo(bValue)
		// Clamp exponent for numerical stability
		exponent = types.ClampExponent(exponent, types.DefaultMaxExponent)
		expTerm := types.Exp(ctx, exponent)
		oneMinus := math.LegacyOneDec().Sub(expTerm)

		if oneMinus.LTE(math.LegacyZeroDec()) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest,
				"trade too large: would deplete market liquidity")
		}

		lnTerm, err := types.Ln(ctx, oneMinus)
		if err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
		}
		newPoolNo = newCost.Add(bValue.Mul(lnTerm))
		sharesOut = newPoolNo.Sub(poolNo)

		// Update Market State (Convert Dec back to Int pointer)
		val := newPoolNo.TruncateInt()
		market.PoolNo = &val
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

	return &types.MsgTradeResponse{
		SharesOut: &sharesInt,
	}, nil
}
