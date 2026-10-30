package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/types"
)

// saveContribution writes a contribution and keeps the deadline index in
// step with it. Every keeper write of a Contribution goes through here so
// the EndBlocker only ever visits contributions that are due (REV-11).
func (k Keeper) saveContribution(ctx context.Context, contrib *types.Contribution) error {
	if err := k.Contribution.Set(ctx, contrib.Id, *contrib); err != nil {
		return err
	}
	return k.refreshDeadlineIndex(ctx, contrib)
}

// refreshDeadlineIndex moves the contribution's deadline-index entry to its
// current earliest pending deadline, or removes it when the contribution is
// not IN_PROGRESS or has nothing pending.
func (k Keeper) refreshDeadlineIndex(ctx context.Context, contrib *types.Contribution) error {
	old, err := k.ContributionNextDeadline.Get(ctx, contrib.Id)
	hasOld := err == nil
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}

	next, ok := int64(0), false
	if contrib.Status == types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return err
		}
		next, ok = nextContributionDeadline(contrib, params.DisputeResolutionEpochs)
	}

	if hasOld && (!ok || old != next) {
		if err := k.ContributionDeadlines.Remove(ctx, collections.Join(old, contrib.Id)); err != nil {
			return err
		}
		if err := k.ContributionNextDeadline.Remove(ctx, contrib.Id); err != nil {
			return err
		}
	}
	if ok && (!hasOld || old != next) {
		if err := k.ContributionDeadlines.Set(ctx, collections.Join(next, contrib.Id)); err != nil {
			return err
		}
		if err := k.ContributionNextDeadline.Set(ctx, contrib.Id, next); err != nil {
			return err
		}
	}
	return nil
}

// clearDeadlineIndex drops a contribution from the deadline index.
func (k Keeper) clearDeadlineIndex(ctx context.Context, id uint64) error {
	old, err := k.ContributionNextDeadline.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := k.ContributionDeadlines.Remove(ctx, collections.Join(old, id)); err != nil {
		return err
	}
	return k.ContributionNextDeadline.Remove(ctx, id)
}

// nextContributionDeadline returns the earliest height at which the
// EndBlocker has something to enforce for this contribution. A DISPUTED
// tranche is due at verification_deadline + dispute_resolution_epochs (the
// param value at the time of the last save; a later save re-indexes it).
func nextContributionDeadline(contrib *types.Contribution, disputeEpochs int64) (int64, bool) {
	var best int64
	found := false
	consider := func(d int64) {
		if d <= 0 {
			return
		}
		if !found || d < best {
			best, found = d, true
		}
	}
	for i := range contrib.Tranches {
		t := &contrib.Tranches[i]
		switch t.Status {
		case types.TrancheStatus_TRANCHE_STATUS_STAKING:
			consider(t.StakeDeadline)
		case types.TrancheStatus_TRANCHE_STATUS_BACKED:
			consider(t.RevealDeadline)
		case types.TrancheStatus_TRANCHE_STATUS_REVEALED:
			consider(t.VerificationDeadline)
		case types.TrancheStatus_TRANCHE_STATUS_DISPUTED:
			if t.VerificationDeadline > 0 {
				consider(t.VerificationDeadline + disputeEpochs)
			}
		}
	}
	return best, found
}

// unlockOrGone releases a DREAM lock recorded by x/reveal. It returns
// (false, nil) when x/rep reports the lock as already gone: the member was
// zeroed (ZeroMember burned the staked DREAM and reset StakedDream, so
// UnlockDREAM returns ErrInsufficientStake) or no longer exists. The caller
// then treats the obligation as settled instead of failing the whole
// contribution forever (REV-01, REV-05, REV-06). Any other error is returned
// so the caller's CacheContext rolls back and the step is retried.
func (k Keeper) unlockOrGone(ctx context.Context, addr sdk.AccAddress, amount math.Int) (bool, error) {
	if !amount.IsPositive() {
		return false, nil
	}
	err := k.repKeeper.UnlockDREAM(ctx, addr, amount)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, reptypes.ErrInsufficientStake) || errors.Is(err, reptypes.ErrMemberNotFound) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Info("reveal lock already released by x/rep (member zeroed or gone)",
			"address", addr.String(), "amount", amount.String(), "reason", err.Error())
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("reveal_lock_gone",
			sdk.NewAttribute("address", addr.String()),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("reason", err.Error()),
		))
		return false, nil
	}
	return false, err
}

// burnLockedOrGone slashes a DREAM lock recorded by x/reveal, burning it out
// of the lock in one step (BurnLockedDREAM). Like unlockOrGone it returns
// (false, nil) when x/rep reports the lock as already gone (zeroed or
// missing member); any other error is returned for the caller's rollback.
func (k Keeper) burnLockedOrGone(ctx context.Context, addr sdk.AccAddress, amount math.Int) (bool, error) {
	if !amount.IsPositive() {
		return false, nil
	}
	err := k.repKeeper.BurnLockedDREAM(ctx, addr, amount)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, reptypes.ErrInsufficientStake) || errors.Is(err, reptypes.ErrMemberNotFound) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Info("reveal lock already released by x/rep (member zeroed or gone)",
			"address", addr.String(), "amount", amount.String(), "reason", err.Error())
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("reveal_lock_gone",
			sdk.NewAttribute("address", addr.String()),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("reason", err.Error()),
		))
		return false, nil
	}
	return false, err
}

// slashHalfBondAndRelease burns 50% of the remaining bond out of the lock and
// releases the rest to the contributor (dispute REJECT and dispute timeout).
// When the lock is already gone (zeroed contributor) nothing is burned.
// Returns the amount burned.
func (k Keeper) slashHalfBondAndRelease(ctx context.Context, contrib *types.Contribution, contributor sdk.AccAddress) (math.Int, error) {
	burned := math.ZeroInt()
	if !contrib.BondRemaining.IsPositive() {
		return burned, nil
	}
	slash := contrib.BondRemaining.Quo(math.NewInt(2))
	slashed, err := k.burnLockedOrGone(ctx, contributor, slash)
	if err != nil {
		return burned, err
	}
	if slashed {
		burned = slash
	}
	if _, err := k.unlockOrGone(ctx, contributor, contrib.BondRemaining.Sub(slash)); err != nil {
		return burned, err
	}
	contrib.BondRemaining = math.ZeroInt()
	return burned, nil
}

// SetContribution is the exported form of saveContribution for callers
// outside the keeper (simulation): writes the record and keeps the deadline
// index in step.
func (k Keeper) SetContribution(ctx context.Context, contrib types.Contribution) error {
	return k.saveContribution(ctx, &contrib)
}
