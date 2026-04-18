package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/rep/types"
)

// ---------------------------------------------------------------------------
// Sentinel SPARK reward pool
// ---------------------------------------------------------------------------
//
// Stage A infrastructure: the pool is a SPARK (uspark) balance held by the
// x/rep module account. It will be fed by spam taxes (Stage B) and drained by
// an epoch-based distribution EndBlocker (Stage D).
//
// The pool does NOT use a separate collections.Item balance tracker; the bank
// balance of uspark on the rep module account IS the pool. SPARK and DREAM use
// different denoms (uspark vs udream), so there's no ambiguity between the
// sentinel reward pool and DREAM-denominated escrow/bonds held in the same
// module account.
//
// Helper methods below exist to keep Stage A call sites clean and to give
// Stage B/D a stable internal API.

// GetSentinelRewardPool returns the current SPARK (uspark) balance of the
// x/rep module account — i.e., the current sentinel reward pool size.
func (k Keeper) GetSentinelRewardPool(ctx context.Context) math.Int {
	repAddr := authtypes.NewModuleAddress(types.ModuleName)
	return k.bankKeeper.GetBalance(ctx, repAddr, types.RewardDenom).Amount
}

// AddToSentinelRewardPool transfers `amount` of SPARK (uspark) from `sender`
// into the x/rep module account, growing the sentinel reward pool. Intended
// for spam-tax collectors in Stage B.
//
// Returns an error if the transfer fails (e.g., insufficient balance). Zero
// or negative amounts are rejected.
func (k Keeper) AddToSentinelRewardPool(
	ctx context.Context,
	sender sdk.AccAddress,
	amount math.Int,
) error {
	if !amount.IsPositive() {
		return fmt.Errorf("sentinel reward pool contribution must be positive: %s", amount)
	}
	coins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, amount))
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.ModuleName, coins)
}
