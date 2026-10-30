package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	"sparkdream/x/season/types"
)

// SetNomination writes a nomination and its (season, id) index entry. Every
// nomination write goes through here so per-season walks (Nominate's dedup,
// the retro phase) never scan earlier seasons (SEA-04). It also keeps the
// failed-payout retry queue in step (SEA-21), so InitGenesis rebuilds it.
func (k Keeper) SetNomination(ctx context.Context, nom types.Nomination) error {
	if err := k.Nomination.Set(ctx, nom.Id, nom); err != nil {
		return err
	}
	if err := k.NominationsBySeason.Set(ctx, collections.Join(nom.Season, nom.Id)); err != nil {
		return err
	}
	retryKey := collections.Join(nom.Season, nom.Id)
	if payoutOwed(nom) {
		return k.RetroPayoutRetry.Set(ctx, retryKey)
	}
	return k.RetroPayoutRetry.Remove(ctx, retryKey)
}

// payoutOwed reports whether a nomination was awarded a positive retro
// reward that has not been paid and was not abandoned (SEA-21). A zero
// award (below the conviction floor, or truncated to zero) is never owed.
func payoutOwed(nom types.Nomination) bool {
	return !nom.Rewarded && !nom.PayoutAbandoned &&
		!nom.RewardAmount.IsNil() && nom.RewardAmount.TruncateInt().IsPositive()
}

// indexNewNomination records a freshly created nomination in the
// duplicate-ref set and the nominator's per-season count (SEA-20).
func (k Keeper) indexNewNomination(ctx context.Context, nom types.Nomination) error {
	if err := k.NominationsByRef.Set(ctx, collections.Join(nom.Season, nom.ContentRef)); err != nil {
		return err
	}
	key := collections.Join(nom.Season, nom.Nominator)
	n, err := k.NominationCountBy.Get(ctx, key)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	return k.NominationCountBy.Set(ctx, key, n+1)
}

// unindexNomination reverses indexNewNomination when a nomination is
// pruned.
func (k Keeper) unindexNomination(ctx context.Context, nom types.Nomination) error {
	if err := k.NominationsByRef.Remove(ctx, collections.Join(nom.Season, nom.ContentRef)); err != nil {
		return err
	}
	key := collections.Join(nom.Season, nom.Nominator)
	n, err := k.NominationCountBy.Get(ctx, key)
	if err != nil {
		return nil
	}
	if n <= 1 {
		return k.NominationCountBy.Remove(ctx, key)
	}
	return k.NominationCountBy.Set(ctx, key, n-1)
}

// walkSeasonNominations walks one season's nominations in id order,
// starting strictly after `after` when it is non-zero.
func (k Keeper) walkSeasonNominations(ctx context.Context, season, after uint64, fn func(types.Nomination) (bool, error)) error {
	rng := collections.NewPrefixedPairRange[uint64, uint64](season)
	if after > 0 {
		rng = rng.StartExclusive(after)
	}
	return k.NominationsBySeason.Walk(ctx, rng, func(key collections.Pair[uint64, uint64]) (bool, error) {
		nom, err := k.Nomination.Get(ctx, key.K2())
		if err != nil {
			// Stale index entry; skip.
			return false, nil
		}
		return fn(nom)
	})
}

// nominationStakeRange bounds a NominationStake walk to the keys of one
// nomination ("<id>/<staker>"). '0' is the byte after '/', so
// ["<id>/", "<id>0") holds exactly the keys that start with "<id>/".
func nominationStakeRange(nominationID uint64) collections.Ranger[string] {
	return new(collections.Range[string]).
		StartInclusive(fmt.Sprintf("%d/", nominationID)).
		EndExclusive(fmt.Sprintf("%d0", nominationID))
}
