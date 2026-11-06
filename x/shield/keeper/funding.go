package keeper

import (
	"context"
	"encoding/hex"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// GetDayFunding returns the amount funded for a given day.
func (k Keeper) GetDayFunding(ctx context.Context, day uint64) math.Int {
	df, err := k.DayFundings.Get(ctx, day)
	if err != nil {
		return math.ZeroInt()
	}
	return df.AmountFunded
}

// SetDayFunding stores the funding amount for a given day.
func (k Keeper) SetDayFunding(ctx context.Context, day uint64, amount math.Int) error {
	return k.DayFundings.Set(ctx, day, types.DayFunding{
		Day:          day,
		AmountFunded: amount,
	})
}

// PruneDayFundings removes day funding entries before cutoffDay, walking only
// the stale key range.
func (k Keeper) PruneDayFundings(ctx context.Context, cutoffDay uint64) error {
	iter, err := k.DayFundings.Iterate(ctx, new(collections.Range[uint64]).EndExclusive(cutoffDay))
	if err != nil {
		return err
	}
	keys, err := iter.Keys()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := k.DayFundings.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// EpochFeesPaid returns the fees the module has paid in the current shield
// epoch.
func (k Keeper) EpochFeesPaid(ctx context.Context) math.Int {
	ledger, err := k.EpochFeeLedger.Get(ctx)
	if err != nil || ledger.Epoch != k.GetCurrentEpoch(ctx) || ledger.Paid.IsNil() {
		return math.ZeroInt()
	}
	return ledger.Paid
}

// IdentityEpochFeesPaid returns the fees the module has paid in the current
// shield epoch for the identity behind a rate-limit nullifier.
func (k Keeper) IdentityEpochFeesPaid(ctx context.Context, rateLimitNullifierHex string) (math.Int, error) {
	paid, err := k.IdentityEpochFees.Get(ctx, collections.Join(k.GetCurrentEpoch(ctx), rateLimitNullifierHex))
	if errors.Is(err, collections.ErrNotFound) {
		return math.ZeroInt(), nil
	}
	return paid, err
}

// ChargeEpochFee books the fee the module is about to pay for msg against the
// per-epoch budget (params.max_fees_per_epoch), or refuses it:
//   - ErrEpochFeeBudgetExhausted when the epoch's paid fees plus fee would
//     pass types.EpochFeeLimit: the whole budget for a PROOF_DOMAIN_GROUP_TREE
//     exec, the budget less the group-tree reserve for any other, so
//     trust-tree traffic cannot starve secret ballots;
//   - ErrIdentityFeeShareExhausted when the fees paid for msg's identity (its
//     rate-limit nullifier, the identity rate limit's key) in this epoch plus
//     fee would pass types.IdentityEpochFeeCap, so one member cannot take the
//     whole budget.
//
// Called by the ante handler right before the module pays the fee, in the
// same ante state.
func (k Keeper) ChargeEpochFee(ctx sdk.Context, fee math.Int, params types.Params, msg *types.MsgShieldedExec) error {
	if fee.IsNil() || !fee.IsPositive() {
		return nil
	}
	if params.MaxFeesPerEpoch.IsNil() {
		return errorsmod.Wrap(types.ErrEpochFeeBudgetExhausted, "no budget")
	}
	groupTree := msg.ProofDomain == types.ProofDomain_PROOF_DOMAIN_GROUP_TREE
	paid := k.EpochFeesPaid(ctx)
	if limit := types.EpochFeeLimit(params, groupTree); paid.Add(fee).GT(limit) {
		return errorsmod.Wrapf(types.ErrEpochFeeBudgetExhausted, "paid %s + fee %s > limit %s", paid, fee, limit)
	}
	identity := hex.EncodeToString(msg.RateLimitNullifier)
	idPaid, err := k.IdentityEpochFeesPaid(ctx, identity)
	if err != nil {
		return err
	}
	if share := types.IdentityEpochFeeCap(params); idPaid.Add(fee).GT(share) {
		return errorsmod.Wrapf(types.ErrIdentityFeeShareExhausted, "paid %s + fee %s > share %s", idPaid, fee, share)
	}
	epoch := k.GetCurrentEpoch(ctx)
	if err := k.IdentityEpochFees.Set(ctx, collections.Join(epoch, identity), idPaid.Add(fee)); err != nil {
		return err
	}
	return k.EpochFeeLedger.Set(ctx, types.EpochFeeLedger{Epoch: epoch, Paid: paid.Add(fee)})
}

// RefundEpochFee unbooks amount of a fee ChargeEpochFee booked in epoch for
// identity (the rate-limit nullifier hex), after the post handler returned
// that part of the fee (declared fee less what the gas actually used cost) to
// the module account. Both the epoch ledger and the identity's share drop by
// amount, saturating at zero. It never crosses an epoch boundary: if epoch is
// no longer the current shield epoch, or the ledger already belongs to a
// later one, nothing is unbooked (the coins were still refunded; the budget
// for a new epoch is fresh anyway). A missing identity entry is left alone.
//
// Called by the post handler in the same tx state as the refund transfer, so
// the coins and the books move, or are discarded, together.
func (k Keeper) RefundEpochFee(ctx sdk.Context, epoch uint64, amount math.Int, identity string) error {
	if amount.IsNil() || !amount.IsPositive() {
		return nil
	}
	if epoch != k.GetCurrentEpoch(ctx) {
		return nil
	}
	ledger, err := k.EpochFeeLedger.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if err == nil && ledger.Epoch == epoch && !ledger.Paid.IsNil() {
		paid := ledger.Paid.Sub(amount)
		if paid.IsNegative() {
			paid = math.ZeroInt()
		}
		if err := k.EpochFeeLedger.Set(ctx, types.EpochFeeLedger{Epoch: epoch, Paid: paid}); err != nil {
			return err
		}
	}
	key := collections.Join(epoch, identity)
	idPaid, err := k.IdentityEpochFees.Get(ctx, key)
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	idPaid = idPaid.Sub(amount)
	if idPaid.IsNegative() {
		idPaid = math.ZeroInt()
	}
	return k.IdentityEpochFees.Set(ctx, key, idPaid)
}

// PruneIdentityEpochFees removes per-identity fee entries for epochs before
// cutoffEpoch, walking only the stale key range.
func (k Keeper) PruneIdentityEpochFees(ctx context.Context, cutoffEpoch uint64) error {
	rng := new(collections.Range[collections.Pair[uint64, string]]).
		EndExclusive(collections.Join(cutoffEpoch, ""))
	iter, err := k.IdentityEpochFees.Iterate(ctx, rng)
	if err != nil {
		return err
	}
	keys, err := iter.Keys()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := k.IdentityEpochFees.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
