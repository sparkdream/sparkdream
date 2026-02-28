package keeper

import (
	"context"

	"cosmossdk.io/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/runtime"

	"sparkdream/x/blog/types"
)

// checkRateLimit checks if an address has exceeded the rate limit for an action.
func (k Keeper) checkRateLimit(ctx context.Context, actionType string, addr sdk.AccAddress, limit uint32) error {
	if limit == 0 {
		return nil // no limit
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentDay := uint64(sdkCtx.BlockTime().Unix() / 86400)

	entry := k.getRateLimitEntry(ctx, actionType, addr.String())
	if entry.Day != currentDay {
		// New day, reset counter
		return nil
	}
	if entry.Count >= limit {
		return types.ErrRateLimitExceeded
	}
	return nil
}

// incrementRateLimit increments the rate limit counter for an action.
func (k Keeper) incrementRateLimit(ctx context.Context, actionType string, addr sdk.AccAddress) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentDay := uint64(sdkCtx.BlockTime().Unix() / 86400)

	entry := k.getRateLimitEntry(ctx, actionType, addr.String())
	if entry.Day != currentDay {
		entry = types.RateLimitEntry{Count: 0, Day: currentDay}
	}
	entry.Count++
	k.setRateLimitEntry(ctx, actionType, addr.String(), entry)
}

func (k Keeper) getRateLimitEntry(ctx context.Context, actionType string, addr string) types.RateLimitEntry {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.RateLimitKey))
	key := []byte(actionType + "/" + addr)
	b := store.Get(key)
	if b == nil {
		return types.RateLimitEntry{}
	}
	var entry types.RateLimitEntry
	k.cdc.MustUnmarshal(b, &entry)
	return entry
}

func (k Keeper) setRateLimitEntry(ctx context.Context, actionType string, addr string, entry types.RateLimitEntry) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.RateLimitKey))
	key := []byte(actionType + "/" + addr)
	b := k.cdc.MustMarshal(&entry)
	store.Set(key, b)
}
