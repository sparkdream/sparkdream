package keeper

import (
	"context"

	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
)

// CountPrefixForTest counts the store keys under a prefix. Tests use it to
// compare rebuilt indexes with what the live chain holds.
func (k Keeper) CountPrefixForTest(ctx context.Context, pfx string) int {
	store := prefix.NewStore(runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx)), []byte(pfx))
	it := storetypes.KVStorePrefixIterator(store, nil)
	defer it.Close()
	n := 0
	for ; it.Valid(); it.Next() {
		n++
	}
	return n
}
