package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/forum/types"
)

// distributeSpamTax splits coins already held by the forum module account
// 50/50 between a burn and a transfer to x/rep's sentinel reward pool. Any
// odd-amount rounding remainder is burned (the larger half is burned). Very
// small amounts (< 2 smallest units per denom) are burned in full.
//
// The pool share goes through repKeeper.AddToSentinelRewardPool rather than a
// module-to-module send. It used to be the latter, back when the pool WAS the
// rep module account's SPARK balance; when x/rep moved the pool to a derived
// sub-address, this send kept targeting the module account and the money
// stopped arriving. Nothing reads that account's SPARK balance, so it
// accumulated there unnoticed and sentinels were underpaid by exactly the
// amount forum users had paid to reward them.
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

	// The sentinel reward pool holds the bond denom only, so the denom
	// decision belongs here, in the split: a non-bond denom (which no current
	// call site produces) is burned in full rather than half-routed to a pool
	// that cannot hold it.
	bondDenom := k.BondDenom(ctx)

	var (
		burnCoins sdk.Coins
		poolCoins sdk.Coins
	)

	for _, c := range coins {
		if !c.Amount.IsPositive() {
			continue
		}
		if c.Denom != bondDenom {
			burnCoins = burnCoins.Add(c)
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

	// Move the pool share into x/rep's sentinel reward pool.
	for _, c := range poolCoins {
		if err := k.repKeeper.AddToSentinelRewardPool(
			ctx, authtypes.NewModuleAddress(types.ModuleName), c.Amount); err != nil {
			return errorsmod.Wrap(err, "failed to transfer spam tax to sentinel reward pool")
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
			sdk.NewAttribute("pool_denom", bondDenom),
		),
	)

	return nil
}
