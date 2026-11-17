package keeper

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k Keeper) coins(ctx context.Context, amount math.Int) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
}

// charge moves amount from payer into the module account.
func (k Keeper) charge(ctx context.Context, payer string, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}
	addr, err := k.accAddr(payer)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, addr, types.ModuleName, k.coins(ctx, amount))
}

// burn destroys amount held by the module account.
func (k Keeper) burn(ctx context.Context, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}
	return k.bankKeeper.BurnCoins(ctx, types.ModuleName, k.coins(ctx, amount))
}

// chargeAndBurn takes the request fee from payer and burns it.
func (k Keeper) chargeAndBurn(ctx context.Context, payer string, amount math.Int) error {
	if err := k.charge(ctx, payer, amount); err != nil {
		return err
	}
	return k.burn(ctx, amount)
}

// deliver sends amount from the module account to recipient. A send that
// cannot be delivered (the address cannot receive, or is empty) goes to the
// community pool instead and emits payout_redirected. delivered reports
// whether the recipient got it.
func (k Keeper) deliver(ctx context.Context, requestID uint64, recipient string, amount math.Int) (delivered bool, err error) {
	if !amount.IsPositive() {
		return true, nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if recipient != "" {
		if addr, aerr := k.accAddr(recipient); aerr == nil {
			// Try the send on a branch, so a failed one leaves nothing behind.
			cacheCtx, write := sdkCtx.CacheContext()
			if serr := k.bankKeeper.SendCoinsFromModuleToAccount(cacheCtx, types.ModuleName, addr, k.coins(ctx, amount)); serr == nil {
				write()
				return true, nil
			}
		}
	}
	if k.late.distr == nil {
		return false, types.ErrNotWired.Wrap("distribution keeper")
	}
	if err := k.late.distr.FundCommunityPool(ctx, k.coins(ctx, amount), k.moduleAddress()); err != nil {
		return false, err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("storage.payout_redirected",
		sdk.NewAttribute("request_id", u64(requestID)),
		sdk.NewAttribute("recipient", recipient),
		sdk.NewAttribute("amount", amount.String()),
	))
	return false, nil
}

// refund returns the request's escrowed copy fee to the requester.
func (k Keeper) refund(ctx context.Context, r *types.Request) error {
	if _, err := k.deliver(ctx, r.Id, r.Requester, r.CopyFee); err != nil {
		return err
	}
	r.CopyFee = math.ZeroInt()
	return nil
}

// payout pays the copy fee to the request's controller if it is still a
// council, and to the community pool otherwise. It records the payee.
func (k Keeper) payout(ctx context.Context, r *types.Request) error {
	recipient := r.Controller
	if recipient != "" && !k.late.commons.IsGroupPolicyAddress(ctx, recipient) {
		recipient = ""
	}
	delivered, err := k.deliver(ctx, r.Id, recipient, r.CopyFee)
	if err != nil {
		return err
	}
	r.PaidTo = ""
	if delivered {
		r.PaidTo = recipient
	}
	r.PaidAmount = r.CopyFee
	r.CopyFee = math.ZeroInt()
	return nil
}
