package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"

	"sparkdream/x/service/types"
)

// MigrateV1ToV2 writes the storage-arweave service type on a chain that
// started before x/storage existed. Genesis defaults do not reach a running
// chain, and x/storage refuses requests until the type is there. A type of
// that name already written by governance is left as it is.
func (k Keeper) MigrateV1ToV2(ctx context.Context) error {
	if _, err := k.ServiceTypes.Get(ctx, types.StorageArweaveServiceType); err == nil {
		return nil
	} else if !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	return k.ServiceTypes.Set(ctx, types.StorageArweaveServiceType, types.DefaultStorageArweaveServiceType(params))
}
