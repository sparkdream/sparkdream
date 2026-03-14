package keeper

import (
	"context"

	"sparkdream/x/shield/types"
)

// GetDKGStateVal returns the DKG state.
func (k Keeper) GetDKGStateVal(ctx context.Context) (types.DKGState, bool) {
	state, err := k.DKGState.Get(ctx)
	if err != nil {
		return types.DKGState{}, false
	}
	return state, true
}

// SetDKGStateVal stores the DKG state.
func (k Keeper) SetDKGStateVal(ctx context.Context, state types.DKGState) error {
	return k.DKGState.Set(ctx, state)
}

// GetDKGContributionVal returns a DKG contribution for a validator.
func (k Keeper) GetDKGContributionVal(ctx context.Context, valAddr string) (types.DKGContribution, bool) {
	c, err := k.DKGContributions.Get(ctx, valAddr)
	if err != nil {
		return types.DKGContribution{}, false
	}
	return c, true
}

// SetDKGContributionVal stores a DKG contribution.
func (k Keeper) SetDKGContributionVal(ctx context.Context, c types.DKGContribution) error {
	return k.DKGContributions.Set(ctx, c.ValidatorAddress, c)
}

// CountDKGContributionsVal returns the number of DKG contributions stored.
func (k Keeper) CountDKGContributionsVal(ctx context.Context) uint64 {
	var count uint64
	iter, err := k.DKGContributions.Iterate(ctx, nil)
	if err != nil {
		return 0
	}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		count++
	}
	return count
}

// ClearDKGContributions removes all DKG contributions (used when starting a new round).
func (k Keeper) ClearDKGContributions(ctx context.Context) error {
	iter, err := k.DKGContributions.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	var keys []string
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			continue
		}
		keys = append(keys, key)
	}
	for _, key := range keys {
		_ = k.DKGContributions.Remove(ctx, key)
	}
	return nil
}

// GetAllDKGContributions returns all stored DKG contributions.
func (k Keeper) GetAllDKGContributions(ctx context.Context) []types.DKGContribution {
	var result []types.DKGContribution
	iter, err := k.DKGContributions.Iterate(ctx, nil)
	if err != nil {
		return nil
	}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		val, err := iter.Value()
		if err != nil {
			continue
		}
		result = append(result, val)
	}
	return result
}

// --- DKG Registration CRUD ---

// GetDKGRegistration returns the DKG registration (pub key) for a validator.
func (k Keeper) GetDKGRegistration(ctx context.Context, valAddr string) (types.DKGContribution, bool) {
	r, err := k.DKGRegistrations.Get(ctx, valAddr)
	if err != nil {
		return types.DKGContribution{}, false
	}
	return r, true
}

// SetDKGRegistration stores a DKG registration.
// FeldmanCommitments[0] = BN256 G1 pub key, ProofOfPossession = Schnorr PoP.
func (k Keeper) SetDKGRegistration(ctx context.Context, r types.DKGContribution) error {
	return k.DKGRegistrations.Set(ctx, r.ValidatorAddress, r)
}

// GetAllDKGRegistrations returns all stored DKG registrations.
func (k Keeper) GetAllDKGRegistrations(ctx context.Context) []types.DKGContribution {
	var result []types.DKGContribution
	iter, err := k.DKGRegistrations.Iterate(ctx, nil)
	if err != nil {
		return nil
	}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		val, err := iter.Value()
		if err != nil {
			continue
		}
		result = append(result, val)
	}
	return result
}

// ClearDKGRegistrations removes all DKG registrations (used when starting a new round).
func (k Keeper) ClearDKGRegistrations(ctx context.Context) error {
	iter, err := k.DKGRegistrations.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	var keys []string
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			continue
		}
		keys = append(keys, key)
	}
	for _, key := range keys {
		_ = k.DKGRegistrations.Remove(ctx, key)
	}
	return nil
}

// GetDKGRegistrationPubKey extracts the BN256 G1 public key bytes from a registration.
func GetDKGRegistrationPubKey(r types.DKGContribution) []byte {
	if len(r.FeldmanCommitments) > 0 {
		return r.FeldmanCommitments[0]
	}
	return nil
}

// isValidatorInDKG checks if a validator address is in the expected DKG participant set.
func isValidatorInDKG(state types.DKGState, valAddr string) bool {
	for _, v := range state.ExpectedValidators {
		if v == valAddr {
			return true
		}
	}
	return false
}

// validatorDKGIndex returns the 1-based index of a validator in the DKG participant set.
func validatorDKGIndex(state types.DKGState, valAddr string) uint32 {
	for i, v := range state.ExpectedValidators {
		if v == valAddr {
			return uint32(i + 1)
		}
	}
	return 0
}

// computeDKGThreshold returns the threshold for the current DKG round.
func computeDKGThreshold(state types.DKGState) uint64 {
	return computeThreshold(state.ThresholdNumerator, state.ThresholdDenominator, uint64(len(state.ExpectedValidators)))
}

// detectValidatorSetDrift compares the DKG expected set against the current bonded set
// and returns the drift percentage (0-100).
func (k Keeper) detectValidatorSetDrift(ctx context.Context, dkgState types.DKGState) uint32 {
	if k.late.stakingKeeper == nil || len(dkgState.ExpectedValidators) == 0 {
		return 0
	}

	bondedVals, err := k.late.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil {
		return 0
	}

	bondedSet := make(map[string]bool, len(bondedVals))
	for _, v := range bondedVals {
		addr := v.GetOperator()
		if addr == "" {
			continue
		}
		bondedSet[addr] = true
	}

	// Count how many DKG participants are still bonded
	overlap := 0
	for _, v := range dkgState.ExpectedValidators {
		if bondedSet[v] {
			overlap++
		}
	}

	total := len(dkgState.ExpectedValidators)
	if total == 0 {
		return 0
	}

	departed := total - overlap
	return uint32(departed * 100 / total)
}
