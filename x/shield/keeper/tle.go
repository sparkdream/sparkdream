package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/shield/types"
)

// GetTLEKeySetVal returns the TLE key set, if it exists.
func (k Keeper) GetTLEKeySetVal(ctx context.Context) (types.TLEKeySet, bool) {
	ks, err := k.TLEKeySet.Get(ctx)
	if err != nil {
		return types.TLEKeySet{}, false
	}
	return ks, true
}

// SetTLEKeySetVal stores the TLE key set.
func (k Keeper) SetTLEKeySetVal(ctx context.Context, ks types.TLEKeySet) error {
	return k.TLEKeySet.Set(ctx, ks)
}

// GetTLEMissCount returns the TLE miss count for a validator.
func (k Keeper) GetTLEMissCount(ctx context.Context, validatorAddr string) uint64 {
	count, err := k.TLEMissCounters.Get(ctx, validatorAddr)
	if err != nil {
		return 0
	}
	return count
}

// SetTLEMissCount stores the TLE miss count for a validator.
func (k Keeper) SetTLEMissCount(ctx context.Context, validatorAddr string, count uint64) error {
	return k.TLEMissCounters.Set(ctx, validatorAddr, count)
}

// IncrementTLEMissCount increments the TLE miss count for a validator and returns the new count.
func (k Keeper) IncrementTLEMissCount(ctx context.Context, validatorAddr string) uint64 {
	count := k.GetTLEMissCount(ctx, validatorAddr)
	count++
	_ = k.SetTLEMissCount(ctx, validatorAddr, count)
	return count
}

// ResetTLEMissCount resets the TLE miss count for a validator.
func (k Keeper) ResetTLEMissCount(ctx context.Context, validatorAddr string) error {
	return k.TLEMissCounters.Remove(ctx, validatorAddr)
}

// GetDecryptionShare returns a decryption share for a given epoch and validator.
func (k Keeper) GetDecryptionShare(ctx context.Context, epoch uint64, validator string) (types.ShieldDecryptionShare, bool) {
	share, err := k.ShieldDecryptionShares.Get(ctx, collections.Join(epoch, validator))
	if err != nil {
		return types.ShieldDecryptionShare{}, false
	}
	return share, true
}

// SetDecryptionShare stores a decryption share.
func (k Keeper) SetDecryptionShare(ctx context.Context, share types.ShieldDecryptionShare) error {
	return k.ShieldDecryptionShares.Set(ctx, collections.Join(share.Epoch, share.Validator), share)
}

// CountDecryptionShares returns the number of shares submitted for an epoch.
func (k Keeper) CountDecryptionShares(ctx context.Context, epoch uint64) uint32 {
	var count uint32
	iter, err := k.ShieldDecryptionShares.Iterate(ctx, nil)
	if err != nil {
		return 0
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}
		if kv.Key.K1() == epoch {
			count++
		}
	}
	return count
}

// GetDecryptionSharesForEpoch returns all decryption shares for a given epoch.
func (k Keeper) GetDecryptionSharesForEpoch(ctx context.Context, epoch uint64) []types.ShieldDecryptionShare {
	var shares []types.ShieldDecryptionShare
	iter, err := k.ShieldDecryptionShares.Iterate(ctx, nil)
	if err != nil {
		return nil
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}
		if kv.Key.K1() == epoch {
			shares = append(shares, kv.Value)
		}
	}
	return shares
}

// IterateTLEMissCounters iterates over all TLE miss counters.
func (k Keeper) IterateTLEMissCounters(ctx context.Context, fn func(validatorAddr string, count uint64) bool) error {
	iter, err := k.TLEMissCounters.Iterate(ctx, nil)
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
