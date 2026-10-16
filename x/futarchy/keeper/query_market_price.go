package keeper

import (
	"context"

	"sparkdream/x/futarchy/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (q queryServer) GetMarketPrice(goCtx context.Context, req *types.QueryGetMarketPriceRequest) (*types.QueryGetMarketPriceResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Fetch market
	market, err := q.k.Market.Get(ctx, req.MarketId)
	if err != nil {
		if errorsmod.IsOf(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "Market %d not found", req.MarketId)
		}
		return nil, err
	}

	if market.Status != "ACTIVE" {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "Market %d is not active (status: %s)", req.MarketId, market.Status)
	}

	// 1. Setup Math Variables
	bValue := *market.BValue

	// PoolYes/PoolNo are *math.Int -> Convert to LegacyDec for LMSR logic
	poolYes := math.LegacyNewDecFromInt(*market.PoolYes)
	poolNo := math.LegacyNewDecFromInt(*market.PoolNo)

	// 2. Parse Amount
	var amountIn math.Int

	if req.Amount == nil || req.Amount.IsNil() {
		// Default to 1000 if not provided (e.g., simulating a small trade)
		amountIn = math.NewInt(1000)
	} else {
		// Dereference pointer
		amountIn = *req.Amount
		if amountIn.IsNegative() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "amount cannot be negative")
		}
	}

	// Calculate current cost
	maxExp := types.DefaultMaxExponent
	var tradingFeeBps uint64
	if params, err := q.k.Params.Get(ctx); err == nil {
		if parsed, err := math.LegacyNewDecFromStr(params.MaxLmsrExponent); err == nil {
			maxExp = parsed
		}
		tradingFeeBps = params.TradingFeeBps
	}
	currentCost, err := types.CalculateLMSRCost(ctx, bValue, poolYes, poolNo, maxExp)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// Deduct the trading fee exactly as MsgTrade does, so the quoted
	// shares_out matches what a trade of `amount` actually delivers. The
	// price per share is still quoted against the gross amount paid.
	amountAfterFee := amountIn
	if tradingFeeBps > 0 {
		feeDec := math.LegacyNewDecFromInt(amountIn).MulInt64(int64(tradingFeeBps)).QuoInt64(10000)
		amountAfterFee = amountIn.Sub(feeDec.TruncateInt())
		if amountAfterFee.LTE(math.ZeroInt()) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "trade amount too small after fee deduction")
		}
	}

	// Calculate new cost with amount after fee
	newCost := currentCost.Add(math.LegacyNewDecFromInt(amountAfterFee))

	var sharesOut math.LegacyDec

	if req.IsYes {
		// Calculate shares for YES outcome
		exponent := poolNo.Sub(newCost).Quo(bValue)
		exponent = types.ClampExponent(exponent, maxExp)
		expTerm := types.Exp(ctx, exponent)
		oneMinus := math.LegacyOneDec().Sub(expTerm)

		if oneMinus.LTE(math.LegacyZeroDec()) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "amount too large: would deplete market")
		}

		lnTerm, err := types.Ln(ctx, oneMinus)
		if err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
		}
		newPoolYes := newCost.Add(bValue.Mul(lnTerm))
		sharesOut = newPoolYes.Sub(poolYes)
	} else {
		// Calculate shares for NO outcome
		exponent := poolYes.Sub(newCost).Quo(bValue)
		exponent = types.ClampExponent(exponent, maxExp)
		expTerm := types.Exp(ctx, exponent)
		oneMinus := math.LegacyOneDec().Sub(expTerm)

		if oneMinus.LTE(math.LegacyZeroDec()) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "amount too large: would deplete market")
		}

		lnTerm, err := types.Ln(ctx, oneMinus)
		if err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
		}
		newPoolNo := newCost.Add(bValue.Mul(lnTerm))
		sharesOut = newPoolNo.Sub(poolNo)
	}

	// Calculate price per share
	var price math.LegacyDec
	if sharesOut.IsPositive() {
		price = math.LegacyNewDecFromInt(amountIn).Quo(sharesOut)
	} else {
		price = math.LegacyZeroDec()
	}

	// Convert sharesOut (Dec) to Int for the response
	sharesOutInt := sharesOut.TruncateInt()

	// Return pointers to the math objects, not strings
	return &types.QueryGetMarketPriceResponse{
		Price:     &price,
		SharesOut: &sharesOutInt,
	}, nil
}
