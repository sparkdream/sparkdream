package keeper

import (
	"context"
	"fmt"
	"strings"

	"sparkdream/x/futarchy/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) Redeem(goCtx context.Context, msg *types.MsgRedeem) (*types.MsgRedeemResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Fetch Market
	market, err := k.Market.Get(ctx, msg.MarketId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "market %d not found", msg.MarketId)
	}

	// 2. Check Resolution Status
	// The market must be in a RESOLVED state (e.g., "RESOLVED_YES" or "RESOLVED_NO")
	// If it is still "ACTIVE", you cannot redeem.
	if !strings.HasPrefix(market.Status, "RESOLVED_") {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "market is not resolved yet (status: %s)", market.Status)
	}

	// 3. Check Redemption Delay
	if market.RedemptionBlocks > 0 {
		unlockHeight := market.ResolutionHeight + market.RedemptionBlocks

		if ctx.BlockHeight() < unlockHeight {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
				"redemption locked until block %d (current %d)", unlockHeight, ctx.BlockHeight())
		}
	}

	// 4. Determine Winning Outcome
	// We expect status to be "RESOLVED_YES" or "RESOLVED_NO"
	winner := ""
	switch market.Status {
	case "RESOLVED_YES":
		winner = "yes"
	case "RESOLVED_NO":
		winner = "no"
	default:
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid market resolution status: %s", market.Status)
	}

	// 4. Check User's Balance of Winning Shares
	// Construct the denom: f/{market_id}/{winner}
	shareDenom := fmt.Sprintf("f/%d/%s", msg.MarketId, winner)
	userAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
	}

	balance := k.bankKeeper.GetBalance(ctx, userAddr, shareDenom)
	if balance.Amount.IsZero() {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "you have no winning shares (%s)", shareDenom)
	}

	// 5. Burn Winning Shares
	// We take the shares from the user and burn them
	err = k.bankKeeper.SendCoinsFromAccountToModule(ctx, userAddr, types.ModuleName, sdk.NewCoins(balance))
	if err != nil {
		return nil, err
	}
	err = k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(balance))
	if err != nil {
		return nil, err
	}

	// 6. Pay Out Collateral 1:1
	// 1 Share = 1 unit of Collateral (e.g., 100 f/1/yes = 100 spark)
	// We use the 'Denom' field we added to the struct
	if market.Denom == "" {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "market denom is missing from state")
	}

	payout := sdk.NewCoin(market.Denom, balance.Amount)
	err = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, userAddr, sdk.NewCoins(payout))
	if err != nil {
		return nil, err
	}

	return &types.MsgRedeemResponse{}, nil
}
