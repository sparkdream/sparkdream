package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/shield/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Registered operations
	for _, reg := range genState.RegisteredOps {
		if err := k.SetShieldedOp(ctx, reg); err != nil {
			return err
		}
	}

	// Used nullifiers
	for _, n := range genState.UsedNullifiers {
		if err := k.UsedNullifiers.Set(ctx, collections.Join3(n.Domain, n.Scope, n.NullifierHex), n); err != nil {
			return err
		}
	}

	// Day fundings
	for _, df := range genState.DayFundings {
		if err := k.DayFundings.Set(ctx, df.Day, df); err != nil {
			return err
		}
	}

	// Verification keys
	for _, vk := range genState.VerificationKeys {
		if err := k.SetVerificationKey(ctx, vk); err != nil {
			return err
		}
	}

	// TLE key set
	if len(genState.TleKeySet.MasterPublicKey) > 0 || len(genState.TleKeySet.ValidatorShares) > 0 {
		if err := k.SetTLEKeySetVal(ctx, genState.TleKeySet); err != nil {
			return err
		}
	}

	// Pending operations
	for _, op := range genState.PendingOps {
		if err := k.SetPendingOp(ctx, op); err != nil {
			return err
		}
	}

	// Shield epoch state
	if genState.ShieldEpochState.CurrentEpoch > 0 || genState.ShieldEpochState.EpochStartHeight > 0 {
		if err := k.SetShieldEpochStateVal(ctx, genState.ShieldEpochState); err != nil {
			return err
		}
	}

	// Decryption keys
	for _, dk := range genState.DecryptionKeys {
		if err := k.SetShieldEpochDecryptionKey(ctx, dk); err != nil {
			return err
		}
	}

	// Next pending op ID sequence
	if genState.NextPendingOpId > 0 {
		for i := uint64(0); i < genState.NextPendingOpId; i++ {
			_, _ = k.NextPendingOpId.Next(ctx)
		}
	}

	// Identity rate limits
	for _, rl := range genState.IdentityRateLimits {
		key := collections.Join(rl.Epoch, rl.RateLimitNullifierHex)
		if err := k.IdentityRateLimits.Set(ctx, key, rl.Count); err != nil {
			return err
		}
	}

	// Pending nullifiers
	for _, pn := range genState.PendingNullifiers {
		if err := k.RecordPendingNullifier(ctx, pn); err != nil {
			return err
		}
	}

	// Decryption shares
	for _, ds := range genState.DecryptionShares {
		if err := k.SetDecryptionShare(ctx, ds); err != nil {
			return err
		}
	}

	// TLE miss counters
	for _, mc := range genState.TleMissCounters {
		if err := k.SetTLEMissCount(ctx, mc.ValidatorAddress, mc.MissCount); err != nil {
			return err
		}
	}

	// DKG state
	if genState.DkgState.Round > 0 {
		if err := k.SetDKGStateVal(ctx, genState.DkgState); err != nil {
			return err
		}
	}

	// DKG contributions
	for _, entry := range genState.DkgContributions {
		if err := k.SetDKGContributionVal(ctx, entry.Contribution); err != nil {
			return err
		}
	}

	// DKG registrations
	for _, entry := range genState.DkgRegistrations {
		if err := k.SetDKGRegistration(ctx, entry.Contribution); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	genesis := types.DefaultGenesis()

	var err error
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Registered operations
	_ = k.IterateShieldedOps(ctx, func(_ string, reg types.ShieldedOpRegistration) bool {
		genesis.RegisteredOps = append(genesis.RegisteredOps, reg)
		return false
	})

	// Used nullifiers
	_ = k.IterateUsedNullifiers(ctx, func(n types.UsedNullifier) bool {
		genesis.UsedNullifiers = append(genesis.UsedNullifiers, n)
		return false
	})

	// Day fundings
	dfIter, err := k.DayFundings.Iterate(ctx, nil)
	if err == nil {
		defer dfIter.Close()
		for ; dfIter.Valid(); dfIter.Next() {
			val, e := dfIter.Value()
			if e == nil {
				genesis.DayFundings = append(genesis.DayFundings, val)
			}
		}
	}

	// Verification keys
	vkIter, err := k.VerificationKeys.Iterate(ctx, nil)
	if err == nil {
		defer vkIter.Close()
		for ; vkIter.Valid(); vkIter.Next() {
			val, e := vkIter.Value()
			if e == nil {
				genesis.VerificationKeys = append(genesis.VerificationKeys, val)
			}
		}
	}

	// TLE key set
	ks, found := k.GetTLEKeySetVal(ctx)
	if found {
		genesis.TleKeySet = ks
	}

	// Pending operations
	_ = k.IteratePendingOps(ctx, func(op types.PendingShieldedOp) bool {
		genesis.PendingOps = append(genesis.PendingOps, op)
		return false
	})

	// Shield epoch state
	state, found := k.GetShieldEpochStateVal(ctx)
	if found {
		genesis.ShieldEpochState = state
	}

	// Decryption keys
	dkIter, err := k.ShieldDecryptionKeys.Iterate(ctx, nil)
	if err == nil {
		defer dkIter.Close()
		for ; dkIter.Valid(); dkIter.Next() {
			val, e := dkIter.Value()
			if e == nil {
				genesis.DecryptionKeys = append(genesis.DecryptionKeys, val)
			}
		}
	}

	// Next pending op ID
	genesis.NextPendingOpId, _ = k.NextPendingOpId.Peek(ctx)

	// Identity rate limits
	rlIter, err := k.IdentityRateLimits.Iterate(ctx, nil)
	if err == nil {
		defer rlIter.Close()
		for ; rlIter.Valid(); rlIter.Next() {
			kv, e := rlIter.KeyValue()
			if e == nil {
				genesis.IdentityRateLimits = append(genesis.IdentityRateLimits, types.IdentityRateLimitEntry{
					Epoch:                 kv.Key.K1(),
					RateLimitNullifierHex: kv.Key.K2(),
					Count:                 kv.Value,
				})
			}
		}
	}

	// Pending nullifiers
	_ = k.IteratePendingNullifiers(ctx, func(nullifierHex string) bool {
		genesis.PendingNullifiers = append(genesis.PendingNullifiers, nullifierHex)
		return false
	})

	// Decryption shares
	dsIter, err := k.ShieldDecryptionShares.Iterate(ctx, nil)
	if err == nil {
		defer dsIter.Close()
		for ; dsIter.Valid(); dsIter.Next() {
			val, e := dsIter.Value()
			if e == nil {
				genesis.DecryptionShares = append(genesis.DecryptionShares, val)
			}
		}
	}

	// TLE miss counters
	_ = k.IterateTLEMissCounters(ctx, func(addr string, count uint64) bool {
		genesis.TleMissCounters = append(genesis.TleMissCounters, types.TLEMissCounterEntry{
			ValidatorAddress: addr,
			MissCount:        count,
		})
		return false
	})

	// DKG state
	dkgState, found := k.GetDKGStateVal(ctx)
	if found {
		genesis.DkgState = dkgState
	}

	// DKG contributions
	for _, c := range k.GetAllDKGContributions(ctx) {
		genesis.DkgContributions = append(genesis.DkgContributions, types.DKGContributionEntry{
			ValidatorAddress: c.ValidatorAddress,
			Contribution:     c,
		})
	}

	// DKG registrations
	for _, r := range k.GetAllDKGRegistrations(ctx) {
		genesis.DkgRegistrations = append(genesis.DkgRegistrations, types.DKGContributionEntry{
			ValidatorAddress: r.ValidatorAddress,
			Contribution:     r,
		})
	}

	return genesis, nil
}
