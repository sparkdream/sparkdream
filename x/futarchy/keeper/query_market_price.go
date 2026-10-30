package keeper

import (
	"context"

	commontypes "sparkdream/x/common/types"
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

	var tradingFeeBps uint64
	if params, err := q.k.Params.Get(ctx); err == nil {
		tradingFeeBps = params.TradingFeeBps
	}

	// Deduct the trading fee exactly as MsgTrade does, so the quoted
	// shares_out matches what a trade of `amount` actually delivers. The
	// price per share is still quoted against the gross amount paid.
	amountAfterFee := amountIn
	if tradingFeeBps > 0 {
		feeDec := math.LegacyNewDecFromInt(amountIn).MulInt64(commontypes.Saturate[int64](tradingFeeBps)).QuoInt64(10000)
		amountAfterFee = amountIn.Sub(feeDec.TruncateInt())
		if amountAfterFee.LTE(math.ZeroInt()) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "trade amount too small after fee deduction")
		}
	}

	_, sharesOut, err := types.SharesForAmount(ctx, bValue, poolYes, poolNo, math.LegacyNewDecFromInt(amountAfterFee), req.IsYes)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
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
