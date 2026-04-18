package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// distributeSpamTax splits coins already held by the forum module account
// 50/50 between a burn and a transfer to the x/rep module account (which
// acts as the sentinel reward pool). Any odd-amount rounding remainder is
// burned (the larger half is burned). Very small amounts (< 2 smallest units
// per denom) are burned in full.
//
// 50/50 split; param promotion deferred (Stage B hardcodes the ratio).
//
// The caller MUST have already moved `coins` into the forum module account
// via SendCoinsFromAccountToModule (or equivalent) before invoking this
// helper — distributeSpamTax does not pull funds from a user account.
//
// `source` is a short tag emitted on the `spam_tax_distributed` event
// (e.g. "post", "flag", "reaction", "edit") for auditing.
func (k Keeper) distributeSpamTax(ctx context.Context, coins sdk.Coins, source string) error {
	if coins.IsZero() || len(coins) == 0 {
		return nil
	}

	var (
		burnCoins sdk.Coins
		poolCoins sdk.Coins
	)

	for _, c := range coins {
		if !c.Amount.IsPositive() {
			continue
		}
		// QuoRaw(2) truncates. Odd amounts → pool gets the smaller half
		// (half), burn gets the larger half (amount - half).
		halfToPool := c.Amount.QuoRaw(2)
		halfToBurn := c.Amount.Sub(halfToPool)

		if halfToPool.IsPositive() {
			poolCoins = poolCoins.Add(sdk.NewCoin(c.Denom, halfToPool))
		}
		if halfToBurn.IsPositive() {
			burnCoins = burnCoins.Add(sdk.NewCoin(c.Denom, halfToBurn))
		}
	}

	// Transfer pool share to x/rep module account (sentinel reward pool).
	if !poolCoins.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, reptypes.ModuleName, poolCoins); err != nil {
			return errorsmod.Wrap(err, "failed to transfer spam tax to reward pool")
		}
	}

	// Burn remainder.
	if !burnCoins.IsZero() {
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, burnCoins); err != nil {
			return errorsmod.Wrap(err, "failed to burn spam tax remainder")
		}
	}

	// Emit audit event with split breakdown.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"spam_tax_distributed",
			sdk.NewAttribute("source", source),
			sdk.NewAttribute("total", coins.String()),
			sdk.NewAttribute("burned", burnCoins.String()),
			sdk.NewAttribute("pooled", poolCoins.String()),
			sdk.NewAttribute("pool_module", reptypes.ModuleName),
		),
	)

	return nil
}
