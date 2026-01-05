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

	// Parse market state
	bValue, err := math.LegacyNewDecFromStr(market.BValue)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "Invalid BValue in state")
	}
	poolYes, err := math.LegacyNewDecFromStr(market.PoolYes)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "Invalid PoolYes in state")
	}
	poolNo, err := math.LegacyNewDecFromStr(market.PoolNo)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "Invalid PoolNo in state")
	}

	// Parse amount (default to 1000 if not provided)
	var amountIn math.Int
	if req.Amount == "" {
		amountIn = math.NewInt(1000)
	} else {
		var ok bool
		amountIn, ok = math.NewIntFromString(req.Amount)
		if !ok {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "invalid amount")
		}
	}

	// Calculate current cost
	currentCost := types.CalculateLMSRCost(ctx, bValue, poolYes, poolNo)

	// Calculate new cost with amount
	newCost := currentCost.Add(math.LegacyNewDecFromInt(amountIn))

	var sharesOut math.LegacyDec

	if req.IsYes {
		// Calculate shares for YES outcome
		exponent := poolNo.Sub(newCost).Quo(bValue)
		exponent = types.ClampExponent(exponent, types.DefaultMaxExponent)
		expTerm := types.Exp(ctx, exponent)
		oneMinus := math.LegacyOneDec().Sub(expTerm)

		if oneMinus.LTE(math.LegacyZeroDec()) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "amount too large: would deplete market")
		}

		lnTerm := types.Ln(ctx, oneMinus)
		newPoolYes := newCost.Add(bValue.Mul(lnTerm))
		sharesOut = newPoolYes.Sub(poolYes)
	} else {
		// Calculate shares for NO outcome
		exponent := poolYes.Sub(newCost).Quo(bValue)
		exponent = types.ClampExponent(exponent, types.DefaultMaxExponent)
		expTerm := types.Exp(ctx, exponent)
		oneMinus := math.LegacyOneDec().Sub(expTerm)

		if oneMinus.LTE(math.LegacyZeroDec()) {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "amount too large: would deplete market")
		}

		lnTerm := types.Ln(ctx, oneMinus)
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

	return &types.QueryGetMarketPriceResponse{
		Price:     price.String(),
		SharesOut: sharesOut.String(),
	}, nil
}
