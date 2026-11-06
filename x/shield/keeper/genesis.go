package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"sparkdream/x/shield/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// The shared anonymous submitter must exist before its first exec:
	// clients sign with its account number, which a just-created account
	// wouldn't have yet.
	if submitter := types.PublicSubmitterAddress(); k.accountKeeper.GetAccount(ctx, submitter) == nil {
		k.accountKeeper.SetAccount(ctx, k.accountKeeper.NewAccountWithAddress(ctx, submitter))
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
	// Launch gate: a production build refuses every exec without the circuit
	// key, and no message can add one later (only a chain upgrade), so an
	// enabled shield with no key is a misconfigured genesis. Fail at chain
	// start rather than run a network where shielded execution never works.
	if genState.Params.Enabled && requireVerificationKey() {
		if vk, found := k.GetVerificationKeyVal(ctx, types.ShieldCircuitID); !found || len(vk.VkBytes) == 0 {
			return errorsmod.Wrapf(types.ErrNoVerificationKey,
				"genesis enables shield without a %s verification key; add the ceremony key or set params.enabled=false", types.ShieldCircuitID)
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

	// Next pending op ID sequence (set directly: one write, not one per id)
	if genState.NextPendingOpId > 0 {
		if err := k.NextPendingOpId.Set(ctx, genState.NextPendingOpId); err != nil {
			return err
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

	// Decryption shares: pairing-verified against the imported key set like
	// every share an ABCI path stores, so a bad genesis share can't corrupt
	// an epoch key.
	for _, ds := range genState.DecryptionShares {
		if err := k.verifyImportedShare(ctx, ds); err != nil {
			return err
		}
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

	// Decryption demands
	for _, d := range genState.DecryptionDemands {
		if err := k.DecryptionDemands.Set(ctx, d.Epoch, d.Height); err != nil {
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

	// DKG complaints
	for _, c := range genState.DkgComplaints {
		if err := k.SetDKGComplaint(ctx, c); err != nil {
			return err
		}
	}

	// TLE liveness ring buffer and judged-epoch counter, so an export/import
	// doesn't forgive accumulated misses.
	for _, p := range genState.TleParticipation {
		if err := k.TLEEpochParticipation.Set(ctx, collections.Join(p.ValidatorAddress, p.Slot), p.Participated); err != nil {
			return err
		}
	}
	if genState.TleJudgedEpochs > 0 {
		if err := k.TLEJudgedEpochs.Set(ctx, genState.TleJudgedEpochs); err != nil {
			return err
		}
	}

	// Fees paid in the current shield epoch
	if !genState.EpochFeeLedger.Paid.IsNil() && genState.EpochFeeLedger.Paid.IsPositive() {
		if err := k.EpochFeeLedger.Set(ctx, genState.EpochFeeLedger); err != nil {
			return err
		}
	}

	// Fees paid per identity in the current shield epoch
	for _, e := range genState.IdentityEpochFees {
		if err := k.IdentityEpochFees.Set(ctx, collections.Join(e.Epoch, e.RateLimitNullifierHex), e.Paid); err != nil {
			return err
		}
	}

	// Retired nullifier domains
	for _, reg := range genState.RetiredDomains {
		if err := k.RetiredDomains.Set(ctx, reg.NullifierDomain, reg); err != nil {
			return err
		}
	}

	return nil
}

// verifyImportedShare checks a genesis decryption share against its
// validator's public share in the key set (the pairing check only; the
// epoch window and demand gate are live-chain rules).
func (k Keeper) verifyImportedShare(ctx context.Context, ds types.ShieldDecryptionShare) error {
	ks, found := k.GetTLEKeySetVal(ctx)
	if !found {
		return errorsmod.Wrapf(types.ErrInvalidDecryptionShare, "genesis share for epoch %d from %s without a TLE key set", ds.Epoch, ds.Validator)
	}
	for _, vs := range ks.ValidatorShares {
		if vs.ValidatorAddress != ds.Validator {
			continue
		}
		if err := verifyDecryptionShare(ds.Share, vs.PublicShareG2, ds.Epoch); err != nil {
			return errorsmod.Wrapf(types.ErrInvalidDecryptionShare, "genesis share for epoch %d from %s: %s", ds.Epoch, ds.Validator, err)
		}
		return nil
	}
	return errorsmod.Wrapf(types.ErrInvalidDecryptionShare, "genesis share for epoch %d from %s, who holds no share in the key set", ds.Epoch, ds.Validator)
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	genesis := types.DefaultGenesis()

	var err error
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Registered operations: exactly what is stored. DefaultGenesis seeds the
	// default set, which would duplicate every op and bring back any that
	// governance deregistered.
	genesis.RegisteredOps = nil
	if err := k.IterateShieldedOps(ctx, func(_ string, reg types.ShieldedOpRegistration) bool {
		genesis.RegisteredOps = append(genesis.RegisteredOps, reg)
		return false
	}); err != nil {
		return nil, err
	}

	// Used nullifiers
	if err := k.IterateUsedNullifiers(ctx, func(n types.UsedNullifier) bool {
		genesis.UsedNullifiers = append(genesis.UsedNullifiers, n)
		return false
	}); err != nil {
		return nil, err
	}

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
	if err := k.IteratePendingOps(ctx, func(op types.PendingShieldedOp) bool {
		genesis.PendingOps = append(genesis.PendingOps, op)
		return false
	}); err != nil {
		return nil, err
	}

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
	if v, getErr := k.NextPendingOpId.Peek(ctx); getErr == nil {
		genesis.NextPendingOpId = v
	} else if !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}

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
	if err := k.IteratePendingNullifiers(ctx, func(nullifierHex string) bool {
		genesis.PendingNullifiers = append(genesis.PendingNullifiers, nullifierHex)
		return false
	}); err != nil {
		return nil, err
	}

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
	if err := k.IterateTLEMissCounters(ctx, func(addr string, count uint64) bool {
		genesis.TleMissCounters = append(genesis.TleMissCounters, types.TLEMissCounterEntry{
			ValidatorAddress: addr,
			MissCount:        count,
		})
		return false
	}); err != nil {
		return nil, err
	}

	// Decryption demands (raw value: keeps the judged sentinel, see demandHeight)
	if err := k.IterateDecryptionDemands(ctx, func(epoch uint64, raw int64) bool {
		genesis.DecryptionDemands = append(genesis.DecryptionDemands, types.DecryptionDemandEntry{
			Epoch:  epoch,
			Height: raw,
		})
		return false
	}); err != nil {
		return nil, err
	}

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

	// DKG complaints
	genesis.DkgComplaints = append(genesis.DkgComplaints, k.GetAllDKGComplaints(ctx)...)

	// TLE liveness ring buffer and judged-epoch counter
	if partIter, err := k.TLEEpochParticipation.Iterate(ctx, nil); err == nil {
		defer partIter.Close()
		for ; partIter.Valid(); partIter.Next() {
			kv, e := partIter.KeyValue()
			if e == nil {
				genesis.TleParticipation = append(genesis.TleParticipation, types.TLEParticipationEntry{
					ValidatorAddress: kv.Key.K1(),
					Slot:             kv.Key.K2(),
					Participated:     kv.Value,
				})
			}
		}
	}
	if v, getErr := k.TLEJudgedEpochs.Get(ctx); getErr == nil {
		genesis.TleJudgedEpochs = v
	} else if !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}

	// Fees paid in the current shield epoch
	if ledger, err := k.EpochFeeLedger.Get(ctx); err == nil {
		genesis.EpochFeeLedger = ledger
	}

	// Fees paid per identity in the current shield epoch
	if err := k.IdentityEpochFees.Walk(ctx, nil, func(key collections.Pair[uint64, string], paid math.Int) (bool, error) {
		genesis.IdentityEpochFees = append(genesis.IdentityEpochFees, types.IdentityEpochFeeEntry{
			Epoch: key.K1(), RateLimitNullifierHex: key.K2(), Paid: paid,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Retired nullifier domains
	if err := k.RetiredDomains.Walk(ctx, nil, func(_ uint32, reg types.ShieldedOpRegistration) (bool, error) {
		genesis.RetiredDomains = append(genesis.RetiredDomains, reg)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return genesis, nil
}
