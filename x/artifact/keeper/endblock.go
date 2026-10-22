package keeper

import (
	"context"
	"fmt"
	"math"
	"slices"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// EndBlocker runs the ordered, capped passes of docs/x-artifact-spec.md
// §7.12. No pass is needed for safety (every message checks expiry and
// hidden status at use); a backlog only delays lock, deposit and record
// release. A failing record is logged, recorded in FailedExpiry and skipped,
// so the EndBlocker never halts the chain.
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	p := k.GetParams(ctx)
	limit := int(p.MaxExpirationsPerBlock)
	t := sdkCtx.BlockTime().Unix()
	h := sdkCtx.BlockHeight()

	k.expirePending(sdkCtx, t, limit)
	k.expireListings(sdkCtx, t, limit)
	k.drainCancelQueues(sdkCtx, limit)
	k.expireOwnerProposals(sdkCtx, t, limit)
	k.expireHides(sdkCtx, h, limit)
	k.drainScrubQueues(sdkCtx, limit)
	k.expireRentals(sdkCtx, t, limit)
	k.expireRentalOffers(sdkCtx, t, limit)
	k.expireUnitListings(sdkCtx, t, limit)
	k.expireTokens(sdkCtx, t, limit)
	k.drainReleaseQueues(sdkCtx, limit)
	k.collectHarbergerTax(sdkCtx, t, limit)
	k.expireOffers(sdkCtx, t, limit)
	k.settleEndedAuctions(sdkCtx, t, limit)
	return nil
}

// dueIDs collects up to limit entries of a (time, id) index with
// time <= cutoff.
func dueIDs(ctx context.Context, ks collections.KeySet[collections.Pair[int64, uint64]], cutoff int64, limit int) []collections.Pair[int64, uint64] {
	rng := new(collections.Range[collections.Pair[int64, uint64]]).EndInclusive(collections.Join(cutoff, uint64(math.MaxUint64)))
	var due []collections.Pair[int64, uint64]
	iter, err := ks.Iterate(ctx, rng)
	if err != nil {
		return nil
	}
	defer iter.Close()
	for ; iter.Valid() && len(due) < limit; iter.Next() {
		if key, err := iter.Key(); err == nil {
			due = append(due, key)
		}
	}
	return due
}

// expireOffers refunds offers past their expiry.
func (k Keeper) expireOffers(ctx sdk.Context, cutoff int64, limit int) {
	for _, key := range dueIDs(ctx, k.OfferExpiry, cutoff, limit) {
		id := key.K2()
		k.safely(ctx, "offer", u64(id), func(c sdk.Context) error {
			o, err := k.Offers.Get(c, id)
			if err != nil {
				return k.OfferExpiry.Remove(c, key)
			}
			return k.removeOffer(c, o, true, "expired")
		}, func() { _ = k.OfferExpiry.Remove(ctx, key) })
	}
}

// settleEndedAuctions settles auctions whose end has passed.
func (k Keeper) settleEndedAuctions(ctx sdk.Context, cutoff int64, limit int) {
	for _, key := range dueIDs(ctx, k.AuctionEnds, cutoff, limit) {
		id := key.K2()
		k.safely(ctx, "auction", u64(id), func(c sdk.Context) error {
			a, err := k.Auctions.Get(c, id)
			if err != nil || a.EndsAt != key.K1() {
				return k.AuctionEnds.Remove(c, key)
			}
			_, err = k.settleAuction(c, a)
			return err
		}, func() { _ = k.AuctionEnds.Remove(ctx, key) })
	}
}

// expireRentals ends rentals whose term has run out. It runs before
// expireTokens so an expiring token's rental is over first.
func (k Keeper) expireRentals(ctx sdk.Context, cutoff int64, limit int) {
	for _, key := range dueTimeTokens(ctx, k.RentalExpiry, cutoff, limit) {
		classID, tokenID := key.K2(), key.K3()
		k.safely(ctx, "rental", fmt.Sprintf("%d/%d", classID, tokenID), func(c sdk.Context) error {
			r, err := k.Rentals.Get(c, collections.Join(classID, tokenID))
			if err != nil || r.ExpiresAt != key.K1() {
				return k.RentalExpiry.Remove(c, key)
			}
			return k.endRental(c, r, "expired")
		}, func() { _ = k.RentalExpiry.Remove(ctx, key) })
	}
}

// expireRentalOffers drops rental offers past their offer window.
func (k Keeper) expireRentalOffers(ctx sdk.Context, cutoff int64, limit int) {
	for _, key := range dueTimeTokens(ctx, k.RentalOfferExpiry, cutoff, limit) {
		classID, tokenID := key.K2(), key.K3()
		k.safely(ctx, "rental_offer", fmt.Sprintf("%d/%d", classID, tokenID), func(c sdk.Context) error {
			o, err := k.RentalOffers.Get(c, collections.Join(classID, tokenID))
			if err != nil || o.ExpiresAt != key.K1() {
				return k.RentalOfferExpiry.Remove(c, key)
			}
			return k.clearRentalOffer(c, classID, tokenID, "expired")
		}, func() { _ = k.RentalOfferExpiry.Remove(ctx, key) })
	}
}

// expireUnitListings removes unit listings past their expiry and unlocks
// their units.
func (k Keeper) expireUnitListings(ctx sdk.Context, cutoff int64, limit int) {
	rng := new(collections.Range[collections.Pair[int64, uint64]]).EndInclusive(collections.Join(cutoff, uint64(math.MaxUint64)))
	var due []collections.Pair[int64, uint64]
	iter, err := k.UnitListingExpiry.Iterate(ctx, rng)
	if err != nil {
		return
	}
	for ; iter.Valid() && len(due) < limit; iter.Next() {
		if key, err := iter.Key(); err == nil {
			due = append(due, key)
		}
	}
	iter.Close()
	for _, key := range due {
		id := key.K2()
		k.safely(ctx, "unit_listing", u64(id), func(c sdk.Context) error {
			l, err := k.UnitListings.Get(c, id)
			if err != nil {
				return k.UnitListingExpiry.Remove(c, key)
			}
			return k.removeUnitListing(c, l, "expired")
		}, func() { _ = k.UnitListingExpiry.Remove(ctx, key) })
	}
}

// expireTokens burns EXPIRY_MODE_BURN tokens whose expires_at has passed,
// refunding the deposit (and any escrowed mint price) to the holder. A
// pending transfer is cancelled first; a listing is removed by destroyToken.
func (k Keeper) expireTokens(ctx sdk.Context, cutoff int64, limit int) {
	for _, key := range dueTimeTokens(ctx, k.TokenExpiry, cutoff, limit) {
		classID, tokenID := key.K2(), key.K3()
		k.safely(ctx, "token_expiry", fmt.Sprintf("%d/%d", classID, tokenID), func(c sdk.Context) error {
			t, err := k.getToken(c, classID, tokenID)
			if err != nil {
				return k.TokenExpiry.Remove(c, key)
			}
			if t.ExpiresAt != key.K1() {
				// Renewed under a stale index entry; drop it.
				return k.TokenExpiry.Remove(c, key)
			}
			cls, err := k.getClass(c, classID)
			if err != nil {
				return err
			}
			if t.Lock == types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER {
				if err := k.resolvePending(c, classID, tokenID, OutcomeCancelled); err != nil {
					return err
				}
				if t, err = k.getToken(c, classID, tokenID); err != nil {
					return err
				}
			}
			if err := k.destroyToken(c, &cls, t, t.Owner, true); err != nil {
				return err
			}
			if err := k.saveClass(c, cls); err != nil {
				return err
			}
			emit(c, types.EventTokenExpired, classAttr(classID), tokenAttr(tokenID),
				sdk.NewAttribute("holder", t.Owner), sdk.NewAttribute("refund", t.Deposit.String()))
			return nil
		}, func() { _ = k.TokenExpiry.Remove(ctx, key) })
	}
}

// drainReleaseQueues pays out the escrowed public-mint prices of a
// generative class after its seed reveal, capped per block like the other
// class queues.
func (k Keeper) drainReleaseQueues(ctx sdk.Context, limit int) {
	k.drainQueue(ctx, k.ClassReleaseQueue, "release", limit,
		func(c sdk.Context, classID, cursor uint64, n int) ([]uint64, bool) {
			return tokenIDsFrom(c, k.Tokens, classID, cursor, n)
		},
		func(c sdk.Context, classID, tokenID uint64) error {
			t, err := k.getToken(c, classID, tokenID)
			if err != nil {
				return nil
			}
			if t.Paid.IsNil() || !t.Paid.IsPositive() {
				return nil
			}
			cls, err := k.getClass(c, classID)
			if err != nil {
				return err
			}
			if err := k.releasePayment(c, cls, &t); err != nil {
				return err
			}
			return k.saveToken(c, t)
		})
}

// safely runs fn in a cached context. On error or panic it logs, emits
// artifact_expiry_error, records the key in FailedExpiry and calls onFail
// (which removes the index entry) so the record cannot block the queue.
func (k Keeper) safely(ctx sdk.Context, pass, key string, fn func(sdk.Context) error, onFail func()) {
	cacheCtx, write := ctx.CacheContext()
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return fn(cacheCtx)
	}()
	if err == nil {
		write() // also re-emits the cached events
		return
	}
	ctx.Logger().Error("artifact endblock: record failed", "pass", pass, "key", key, "error", err)
	_ = k.FailedExpiry.Set(ctx, collections.Join(pass, key))
	if onFail != nil {
		onFail()
	}
	emit(ctx, types.EventExpiryError, sdk.NewAttribute("pass", pass),
		sdk.NewAttribute("key", key), sdk.NewAttribute("error", err.Error()))
}

// dueTimeTokens collects up to limit entries of a (time, class, token) index
// with time <= cutoff.
func dueTimeTokens(ctx context.Context, ks collections.KeySet[TimeTokenKey], cutoff int64, limit int) []TimeTokenKey {
	rng := new(collections.Range[TimeTokenKey]).EndInclusive(collections.Join3(cutoff, uint64(math.MaxUint64), uint64(math.MaxUint64)))
	var out []TimeTokenKey
	iter, err := ks.Iterate(ctx, rng)
	if err != nil {
		return nil
	}
	defer iter.Close()
	for ; iter.Valid() && len(out) < limit; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			break
		}
		out = append(out, key)
	}
	return out
}

func (k Keeper) expirePending(ctx sdk.Context, cutoff int64, limit int) {
	for _, key := range dueTimeTokens(ctx, k.PendingExpiry, cutoff, limit) {
		classID, tokenID := key.K2(), key.K3()
		k.safely(ctx, "pending", fmt.Sprintf("%d/%d", classID, tokenID), func(c sdk.Context) error {
			return k.resolvePending(c, classID, tokenID, OutcomeExpired)
		}, func() { _ = k.PendingExpiry.Remove(ctx, key) })
	}
}

func (k Keeper) expireListings(ctx sdk.Context, cutoff int64, limit int) {
	for _, key := range dueTimeTokens(ctx, k.ListingExpiry, cutoff, limit) {
		classID, tokenID := key.K2(), key.K3()
		k.safely(ctx, "listing", fmt.Sprintf("%d/%d", classID, tokenID), func(c sdk.Context) error {
			return k.removeListing(c, classID, tokenID, "expired")
		}, func() { _ = k.ListingExpiry.Remove(ctx, key) })
	}
}

func (k Keeper) expireOwnerProposals(ctx sdk.Context, cutoff int64, limit int) {
	rng := new(collections.Range[collections.Pair[int64, uint64]]).EndInclusive(collections.Join(cutoff, uint64(math.MaxUint64)))
	var due []collections.Pair[int64, uint64]
	iter, err := k.PendingOwnerExpiry.Iterate(ctx, rng)
	if err != nil {
		return
	}
	for ; iter.Valid() && len(due) < limit; iter.Next() {
		if key, err := iter.Key(); err == nil {
			due = append(due, key)
		}
	}
	iter.Close()
	for _, key := range due {
		classID := key.K2()
		k.safely(ctx, "owner", u64(classID), func(c sdk.Context) error {
			po, err := k.PendingClassOwners.Get(c, classID)
			if err != nil {
				return k.PendingOwnerExpiry.Remove(c, key)
			}
			if err := k.clearPendingOwner(c, classID); err != nil {
				return err
			}
			emit(c, types.EventClassOwnerExpired, classAttr(classID), sdk.NewAttribute("to", po.ProposedOwner))
			return nil
		}, func() { _ = k.PendingOwnerExpiry.Remove(ctx, key) })
	}
}

func (k Keeper) expireHides(ctx sdk.Context, cutoff int64, limit int) {
	rng := new(collections.Range[collections.Pair[int64, uint64]]).EndInclusive(collections.Join(cutoff, uint64(math.MaxUint64)))
	var due []collections.Pair[int64, uint64]
	iter, err := k.HideExpiry.Iterate(ctx, rng)
	if err != nil {
		return
	}
	for ; iter.Valid() && len(due) < limit; iter.Next() {
		if key, err := iter.Key(); err == nil {
			due = append(due, key)
		}
	}
	iter.Close()
	for _, key := range due {
		id := key.K2()
		k.safely(ctx, "hide", u64(id), func(c sdk.Context) error {
			hr, err := k.HideRecords.Get(c, id)
			if err != nil {
				return k.HideExpiry.Remove(c, key)
			}
			return k.expireHide(c, hr)
		}, func() { _ = k.HideExpiry.Remove(ctx, key) })
	}
}

// drainQueue advances class cursors in queue, spending at most limit visits
// across all queued classes. visit is called per token id at or after the
// cursor; it must be idempotent.
func (k Keeper) drainQueue(ctx sdk.Context, queue collections.Map[uint64, uint64], name string, limit int,
	nextIDs func(sdk.Context, uint64, uint64, int) ([]uint64, bool), visit func(sdk.Context, uint64, uint64) error,
) {
	type entry struct{ classID, cursor uint64 }
	var queued []entry
	iter, err := queue.Iterate(ctx, nil)
	if err != nil {
		return
	}
	for ; iter.Valid(); iter.Next() {
		kv, err := iter.KeyValue()
		if err == nil {
			queued = append(queued, entry{kv.Key, kv.Value})
		}
	}
	iter.Close()

	budget := limit
	for _, e := range queued {
		if budget <= 0 {
			return
		}
		ids, done := nextIDs(ctx, e.classID, e.cursor, budget)
		budget -= len(ids)
		classID := e.classID
		k.safely(ctx, name, u64(classID), func(c sdk.Context) error {
			for _, id := range ids {
				if err := visit(c, classID, id); err != nil {
					return err
				}
			}
			cursor := e.cursor
			if len(ids) > 0 {
				cursor = ids[len(ids)-1] + 1
			}
			emit(c, types.EventClassDrainProgress, classAttr(classID), sdk.NewAttribute("queue", name),
				sdk.NewAttribute("cursor", u64(cursor)), sdk.NewAttribute("done", fmt.Sprintf("%t", done)))
			if done {
				return queue.Remove(c, classID)
			}
			return queue.Set(c, classID, cursor)
		}, func() { _ = queue.Remove(ctx, classID) })
	}
}

// tokenIDsFrom returns up to limit ids >= cursor present in m for classID,
// and whether the class prefix is exhausted.
func tokenIDsFrom[V any](ctx context.Context, m collections.Map[U64Pair, V], classID, cursor uint64, limit int) ([]uint64, bool) {
	rng := collections.NewPrefixedPairRange[uint64, uint64](classID).StartInclusive(cursor)
	iter, err := m.Iterate(ctx, rng)
	if err != nil {
		return nil, true
	}
	defer iter.Close()
	var ids []uint64
	for ; iter.Valid(); iter.Next() {
		if len(ids) >= limit {
			return ids, false
		}
		key, err := iter.Key()
		if err != nil {
			return ids, true
		}
		ids = append(ids, key.K2())
	}
	return ids, true
}

func (k Keeper) drainCancelQueues(ctx sdk.Context, limit int) {
	k.drainQueue(ctx, k.ClassCancelQueue, "cancel", limit,
		func(c sdk.Context, classID, cursor uint64, n int) ([]uint64, bool) {
			return tokenIDsFrom(c, k.Listings, classID, cursor, n)
		},
		func(c sdk.Context, classID, tokenID uint64) error {
			return k.removeListing(c, classID, tokenID, "hidden")
		})
}

// drainScrubQueues scrubs every token and pending mint of a scrubbed class.
// Tokens and pending mints share the token-id space, so one cursor covers
// both: each step takes up to n ids from each store and advances to the
// smaller frontier (re-scrubbing is idempotent).
func (k Keeper) drainScrubQueues(ctx sdk.Context, limit int) {
	k.drainQueue(ctx, k.ClassScrubQueue, "scrub", limit,
		func(c sdk.Context, classID, cursor uint64, n int) ([]uint64, bool) {
			tIDs, tDone := tokenIDsFrom(c, k.Tokens, classID, cursor, n)
			pIDs, pDone := tokenIDsFrom(c, k.PendingMints, classID, cursor, n)
			eIDs, eDone := tokenIDsFrom(c, k.Editions, classID, cursor, n)
			frontier := uint64(math.MaxUint64)
			for _, part := range []struct {
				ids  []uint64
				done bool
			}{{tIDs, tDone}, {pIDs, pDone}, {eIDs, eDone}} {
				if !part.done && len(part.ids) > 0 && part.ids[len(part.ids)-1] < frontier {
					frontier = part.ids[len(part.ids)-1]
				}
			}
			seen := map[uint64]bool{}
			var ids []uint64
			for _, id := range append(append(tIDs, pIDs...), eIDs...) {
				if id <= frontier && !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			slices.Sort(ids)
			return ids, tDone && pDone && eDone
		},
		func(c sdk.Context, classID, tokenID uint64) error {
			key := collections.Join(classID, tokenID)
			if t, err := k.Tokens.Get(c, key); err == nil {
				scrubToken(&t)
				return k.saveToken(c, t)
			}
			if pm, err := k.PendingMints.Get(c, key); err == nil {
				pm.Metadata = types.ScrubbedMetadata()
				return k.PendingMints.Set(c, key, pm)
			}
			if e, err := k.Editions.Get(c, key); err == nil {
				scrubEdition(&e)
				return k.saveEdition(c, e)
			}
			return nil
		})
}
