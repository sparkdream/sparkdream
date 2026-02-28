package keeper

import (
	"context"
	"encoding/binary"

	"cosmossdk.io/math"
	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

// GetAnonSubsidyUsed returns the amount of subsidy used in the current epoch.
func (k Keeper) GetAnonSubsidyUsed(ctx context.Context, epoch uint64) sdk.Coin {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonSubsidyKey))
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, epoch)
	bz := store.Get(key)
	if bz == nil {
		return sdk.NewCoin(types.DefaultFeeDenom, math.ZeroInt())
	}
	var coin sdk.Coin
	k.cdc.MustUnmarshal(bz, &coin)
	return coin
}

// SetAnonSubsidyUsed sets the subsidy amount used for the given epoch.
func (k Keeper) SetAnonSubsidyUsed(ctx context.Context, epoch uint64, used sdk.Coin) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonSubsidyKey))
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, epoch)
	bz := k.cdc.MustMarshal(&used)
	store.Set(key, bz)
}

// IsApprovedRelay checks if the given address is an approved relay for anonymous subsidy.
func (k Keeper) IsApprovedRelay(params types.Params, addr string) bool {
	for _, relay := range params.AnonSubsidyApprovedRelays {
		if relay == addr {
			return true
		}
	}
	return false
}

// TrySubsidizeAnonymousAction attempts to subsidize the gas + fees for an anonymous
// action if the submitter is an approved relay and budget remains in this epoch.
// Returns the amount subsidized (may be zero if not eligible or budget exhausted).
func (k Keeper) TrySubsidizeAnonymousAction(ctx context.Context, params types.Params, submitter string, feeCost sdk.Coin, epoch uint64) sdk.Coin {
	// Check if subsidy is configured
	if !params.AnonSubsidyBudgetPerEpoch.IsPositive() {
		return sdk.NewCoin(feeCost.Denom, math.ZeroInt())
	}

	// Check if submitter is an approved relay
	if !k.IsApprovedRelay(params, submitter) {
		return sdk.NewCoin(feeCost.Denom, math.ZeroInt())
	}

	// Cap per-action subsidy
	actionSubsidy := feeCost
	if params.AnonSubsidyMaxPerPost.IsPositive() && feeCost.IsGTE(params.AnonSubsidyMaxPerPost) {
		actionSubsidy = params.AnonSubsidyMaxPerPost
	}

	// Check epoch budget remaining
	used := k.GetAnonSubsidyUsed(ctx, epoch)
	remaining := params.AnonSubsidyBudgetPerEpoch.Sub(used)
	if !remaining.IsPositive() {
		return sdk.NewCoin(feeCost.Denom, math.ZeroInt()) // budget exhausted
	}

	// Cap to remaining budget
	if actionSubsidy.IsGTE(remaining) {
		actionSubsidy = remaining
	}

	// Update used amount
	newUsed := used.Add(actionSubsidy)
	k.SetAnonSubsidyUsed(ctx, epoch, newUsed)

	return actionSubsidy
}
