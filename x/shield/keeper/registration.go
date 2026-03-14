package keeper

import (
	"context"

	"sparkdream/x/shield/types"
)

// GetShieldedOp returns the registration for a given message type URL.
func (k Keeper) GetShieldedOp(ctx context.Context, typeURL string) (types.ShieldedOpRegistration, bool) {
	reg, err := k.ShieldedOps.Get(ctx, typeURL)
	if err != nil {
		return types.ShieldedOpRegistration{}, false
	}
	return reg, true
}

// SetShieldedOp stores a shielded operation registration.
func (k Keeper) SetShieldedOp(ctx context.Context, reg types.ShieldedOpRegistration) error {
	return k.ShieldedOps.Set(ctx, reg.MessageTypeUrl, reg)
}

// DeleteShieldedOp removes a shielded operation registration.
func (k Keeper) DeleteShieldedOp(ctx context.Context, typeURL string) error {
	return k.ShieldedOps.Remove(ctx, typeURL)
}

// IterateShieldedOps iterates over all registered shielded operations.
func (k Keeper) IterateShieldedOps(ctx context.Context, fn func(typeURL string, reg types.ShieldedOpRegistration) bool) error {
	iter, err := k.ShieldedOps.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return err
		}
		if fn(kv.Key, kv.Value) {
			break
		}
	}
	return nil
}
