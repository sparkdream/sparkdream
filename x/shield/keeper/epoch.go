package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/shield/types"
)

// GetCurrentEpoch returns the current shield epoch number.
func (k Keeper) GetCurrentEpoch(ctx context.Context) uint64 {
	state, err := k.ShieldEpochState.Get(ctx)
	if err != nil {
		return 0
	}
	return state.CurrentEpoch
}

// GetShieldEpochStateVal returns the full shield epoch state.
func (k Keeper) GetShieldEpochStateVal(ctx context.Context) (types.ShieldEpochState, bool) {
	state, err := k.ShieldEpochState.Get(ctx)
	if err != nil {
		return types.ShieldEpochState{}, false
	}
	return state, true
}

// SetShieldEpochStateVal stores the shield epoch state.
func (k Keeper) SetShieldEpochStateVal(ctx context.Context, state types.ShieldEpochState) error {
	return k.ShieldEpochState.Set(ctx, state)
}

// GetShieldEpochDecryptionKeyVal returns the decryption key for a given epoch.
func (k Keeper) GetShieldEpochDecryptionKeyVal(ctx context.Context, epoch uint64) (types.ShieldEpochDecryptionKey, bool) {
	key, err := k.ShieldDecryptionKeys.Get(ctx, epoch)
	if err != nil {
		return types.ShieldEpochDecryptionKey{}, false
	}
	return key, true
}

// SetShieldEpochDecryptionKey stores a decryption key for an epoch.
func (k Keeper) SetShieldEpochDecryptionKey(ctx context.Context, key types.ShieldEpochDecryptionKey) error {
	return k.ShieldDecryptionKeys.Set(ctx, key.Epoch, key)
}

// PruneDecryptionState removes decryption keys and shares older than cutoffEpoch.
func (k Keeper) PruneDecryptionState(ctx context.Context, cutoffEpoch uint64) error {
	// Prune decryption keys
	keyIter, err := k.ShieldDecryptionKeys.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer keyIter.Close()

	var keysToDelete []uint64
	for ; keyIter.Valid(); keyIter.Next() {
		key, err := keyIter.Key()
		if err != nil {
			return err
		}
		if key < cutoffEpoch {
			keysToDelete = append(keysToDelete, key)
		}
	}
	for _, key := range keysToDelete {
		if err := k.ShieldDecryptionKeys.Remove(ctx, key); err != nil {
			return err
		}
	}

	// Prune decryption shares
	shareIter, err := k.ShieldDecryptionShares.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer shareIter.Close()

	type shareKey struct {
		epoch     uint64
		validator string
	}
	var sharesToDelete []shareKey
	for ; shareIter.Valid(); shareIter.Next() {
		kv, err := shareIter.KeyValue()
		if err != nil {
			return err
		}
		epoch := kv.Key.K1()
		if epoch < cutoffEpoch {
			sharesToDelete = append(sharesToDelete, shareKey{epoch: epoch, validator: kv.Key.K2()})
		}
	}
	for _, sk := range sharesToDelete {
		if err := k.ShieldDecryptionShares.Remove(ctx, collections.Join(sk.epoch, sk.validator)); err != nil {
			return err
		}
	}

	return nil
}
