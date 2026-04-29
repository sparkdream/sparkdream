package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/futarchy/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) Redeem(goCtx context.Context, msg *types.MsgRedeem) (*types.MsgRedeemResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	market, err := k.Market.Get(ctx, msg.MarketId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "market %d not found", msg.MarketId)
	}

	// Markets eligible for redemption:
	//  - RESOLVED_YES / RESOLVED_NO: winning shares pay 1:1.
	//  - CANCELLED / RESOLVED_INVALID: shares pay at the snapshotted LMSR
	//    settlement price (FUTARCHY-S2-1 trapped-funds fix).
	switch market.Status {
	case "RESOLVED_YES", "RESOLVED_NO":
		// winner-pays-1:1 path, handled below.
	case "CANCELLED", "RESOLVED_INVALID":
		// pro-rata settlement path, handled below.
	default:
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "market is not resolved yet (status: %s)", market.Status)
	}

	if market.RedemptionBlocks > 0 {
		unlockHeight := market.ResolutionHeight + market.RedemptionBlocks
		if ctx.BlockHeight() < unlockHeight {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
				"redemption locked until block %d (current %d)", unlockHeight, ctx.BlockHeight())
		}
	}

	if market.Denom == "" {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "market denom is missing from state")
	}

	userAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
	}

	if market.Status == "RESOLVED_YES" || market.Status == "RESOLVED_NO" {
		return k.redeemWinning(goCtx, ctx, market, msg.MarketId, userAddr)
	}
	return k.redeemSettled(goCtx, ctx, market, msg.MarketId, userAddr)
}

// redeemWinning pays 1 spark per winning share (RESOLVED_YES / RESOLVED_NO).
func (k msgServer) redeemWinning(goCtx context.Context, ctx sdk.Context, market types.Market, marketID uint64, userAddr sdk.AccAddress) (*types.MsgRedeemResponse, error) {
	winner := "no"
	if market.Status == "RESOLVED_YES" {
		winner = "yes"
	}
	shareDenom := fmt.Sprintf("f/%d/%s", marketID, winner)

	balance := k.bankKeeper.GetBalance(ctx, userAddr, shareDenom)
	if balance.Amount.IsZero() {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "you have no winning shares (%s)", shareDenom)
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, userAddr, types.ModuleName, sdk.NewCoins(balance)); err != nil {
		return nil, err
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(balance)); err != nil {
		return nil, err
	}

	payout := sdk.NewCoin(market.Denom, balance.Amount)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, userAddr, sdk.NewCoins(payout)); err != nil {
		return nil, err
	}

	return &types.MsgRedeemResponse{}, nil
}

// redeemSettled pays YES shares at SettlementPriceYes and NO shares at
// (1 - SettlementPriceYes). Used for CANCELLED markets and RESOLVED_INVALID
// markets with outstanding shares.
func (k msgServer) redeemSettled(goCtx context.Context, ctx sdk.Context, market types.Market, marketID uint64, userAddr sdk.AccAddress) (*types.MsgRedeemResponse, error) {
	yesDenom := fmt.Sprintf("f/%d/yes", marketID)
	noDenom := fmt.Sprintf("f/%d/no", marketID)

	yesBal := k.bankKeeper.GetBalance(ctx, userAddr, yesDenom)
	noBal := k.bankKeeper.GetBalance(ctx, userAddr, noDenom)
	if yesBal.Amount.IsZero() && noBal.Amount.IsZero() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInsufficientFunds, "no shares to redeem")
	}

	// Both pools were zero at cancel/INVALID time → no shares were ever
	// minted, so SettlementPriceYes is unset. The user-balance check above
	// guards against a stale share denom appearing here, but be defensive.
	if market.SettlementPriceYes == nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "market %d has no settlement price snapshot", marketID)
	}
	pYes := *market.SettlementPriceYes
	pNo := math.LegacyOneDec().Sub(pYes)

	// Burn whatever the user holds and pay out the LMSR settlement value.
	var coinsToBurn sdk.Coins
	if yesBal.Amount.IsPositive() {
		coinsToBurn = coinsToBurn.Add(yesBal)
	}
	if noBal.Amount.IsPositive() {
		coinsToBurn = coinsToBurn.Add(noBal)
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, userAddr, types.ModuleName, coinsToBurn); err != nil {
		return nil, err
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coinsToBurn); err != nil {
		return nil, err
	}

	yesPayout := pYes.MulInt(yesBal.Amount).TruncateInt()
	noPayout := pNo.MulInt(noBal.Amount).TruncateInt()
	payoutAmount := yesPayout.Add(noPayout)
	if !payoutAmount.IsPositive() {
		// Truncation reduced both to zero (e.g., one share at price < 1).
		// Burn happened anyway — accept the dust loss.
		return &types.MsgRedeemResponse{}, nil
	}

	payout := sdk.NewCoin(market.Denom, payoutAmount)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, userAddr, sdk.NewCoins(payout)); err != nil {
		return nil, err
	}

	return &types.MsgRedeemResponse{}, nil
}
