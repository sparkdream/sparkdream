package keeper

import "context"

// CountVaultsOfSigner exposes countVaultsOfSigner to the external tests.
func (k Keeper) CountVaultsOfSigner(ctx context.Context, addr string, exclude uint64) (int, error) {
	return k.countVaultsOfSigner(ctx, addr, exclude)
}
