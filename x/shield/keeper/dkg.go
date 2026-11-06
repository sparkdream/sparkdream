package keeper

import (
	"context"
	commontypes "sparkdream/x/common/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/collections"

	"sparkdream/x/shield/types"
)

// newDKGState opens a round in REGISTERING with the DKG window split into
// the four ceremony phases: register keys, deal contributions, complain,
// justify.
func newDKGState(round uint64, height int64, windowBlocks, numerator, denominator uint64, expected []string) types.DKGState {
	quarter := commontypes.Saturate[int64](windowBlocks / 4)
	if quarter < 1 {
		quarter = 1
	}
	return types.DKGState{
		Round:                 round,
		Phase:                 types.DKGPhase_DKG_PHASE_REGISTERING,
		OpenAtHeight:          height,
		RegistrationDeadline:  height + quarter,
		ContributionDeadline:  height + 2*quarter,
		ComplaintDeadline:     height + 3*quarter,
		JustificationDeadline: height + 4*quarter,
		ThresholdNumerator:    numerator,
		ThresholdDenominator:  denominator,
		ExpectedValidators:    expected,
	}
}

// clearDKGRound removes every per-round record: registrations,
// contributions and complaints.
func (k Keeper) clearDKGRound(ctx context.Context) error {
	if err := k.ClearDKGContributions(ctx); err != nil {
		return err
	}
	if err := k.ClearDKGRegistrations(ctx); err != nil {
		return err
	}
	return k.ClearDKGComplaints(ctx)
}

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
		if err := k.DKGContributions.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("ClearDKGContributions: DKGContributions.Remove failed", "err", err)
		}
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
		if err := k.DKGRegistrations.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("ClearDKGRegistrations: DKGRegistrations.Remove failed", "err", err)
		}
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

// --- DKG complaints ---

// GetDKGComplaint returns complainer's complaint against dealer, if any.
func (k Keeper) GetDKGComplaint(ctx context.Context, dealer, complainer string) (types.DKGComplaint, bool) {
	c, err := k.DKGComplaints.Get(ctx, collections.Join(dealer, complainer))
	if err != nil {
		return types.DKGComplaint{}, false
	}
	return c, true
}

// SetDKGComplaint stores a complaint (or its justification).
func (k Keeper) SetDKGComplaint(ctx context.Context, c types.DKGComplaint) error {
	return k.DKGComplaints.Set(ctx, collections.Join(c.DealerAddress, c.ComplainerAddress), c)
}

// GetAllDKGComplaints returns every complaint of the current round.
func (k Keeper) GetAllDKGComplaints(ctx context.Context) []types.DKGComplaint {
	iter, err := k.DKGComplaints.Iterate(ctx, nil)
	if err != nil {
		return nil
	}
	defer iter.Close()
	vals, err := iter.Values()
	if err != nil {
		return nil
	}
	return vals
}

// ComplaintsAgainst returns the complaints filed against one dealer.
func (k Keeper) ComplaintsAgainst(ctx context.Context, dealer string) []types.DKGComplaint {
	iter, err := k.DKGComplaints.Iterate(ctx, collections.NewPrefixedPairRange[string, string](dealer))
	if err != nil {
		return nil
	}
	defer iter.Close()
	vals, err := iter.Values()
	if err != nil {
		return nil
	}
	return vals
}

// ClearDKGComplaints removes every complaint (new round).
func (k Keeper) ClearDKGComplaints(ctx context.Context) error {
	iter, err := k.DKGComplaints.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	keys, err := iter.Keys()
	iter.Close()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := k.DKGComplaints.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// isDisqualifiedDealer reports whether a dealer was excluded from the key set.
func isDisqualifiedDealer(state types.DKGState, valAddr string) bool {
	for _, d := range state.DisqualifiedDealers {
		if d == valAddr {
			return true
		}
	}
	return false
}

// QualifiedDKGContributions returns the stored contributions minus the
// disqualified dealers': what the key set is aggregated from.
func (k Keeper) QualifiedDKGContributions(ctx context.Context, state types.DKGState) []types.DKGContribution {
	var out []types.DKGContribution
	for _, c := range k.GetAllDKGContributions(ctx) {
		if !isDisqualifiedDealer(state, c.ValidatorAddress) {
			out = append(out, c)
		}
	}
	return out
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
	return commontypes.Saturate[uint32](departed * 100 / total)
}
