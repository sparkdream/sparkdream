package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
)

// Pending identity challenges are stored once in PendingIdChallenges and
// indexed three ways: by expiry (the EndBlocker range-walks it instead of
// scanning every challenge each block), by peer (peer-removal cleanup) and
// as a per-peer count (MaxPendingChallengesPerPeer). Every write goes
// through setPendingChallenge / removePendingChallenge so the indexes
// cannot drift from the records.

// setPendingChallenge stores c under (claimed_address, peer) and indexes it,
// replacing (and de-indexing) any earlier challenge for the same key.
func (k Keeper) setPendingChallenge(ctx context.Context, c types.PendingIdentityChallenge) error {
	key := collections.Join(c.ClaimedAddress, c.ClaimantChainPeerId)
	if prev, err := k.PendingIdChallenges.Get(ctx, key); err == nil {
		if err := k.PendingIdChallengeExp.Remove(ctx, collections.Join3(prev.ExpiresAt, prev.ClaimedAddress, prev.ClaimantChainPeerId)); err != nil {
			return err
		}
	} else {
		n, getErr := k.PendingIdChallengeCount.Get(ctx, c.ClaimantChainPeerId)
		if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
			return getErr
		}
		if err := k.PendingIdChallengeCount.Set(ctx, c.ClaimantChainPeerId, n+1); err != nil {
			return err
		}
	}
	if err := k.PendingIdChallenges.Set(ctx, key, c); err != nil {
		return err
	}
	if err := k.PendingIdChallengesByPeer.Set(ctx, collections.Join(c.ClaimantChainPeerId, c.ClaimedAddress)); err != nil {
		return err
	}
	return k.PendingIdChallengeExp.Set(ctx, collections.Join3(c.ExpiresAt, c.ClaimedAddress, c.ClaimantChainPeerId))
}

// removePendingChallenge deletes the challenge for (claimed, peer) and its
// index entries. A missing challenge is a no-op.
func (k Keeper) removePendingChallenge(ctx context.Context, claimed, peerID string) {
	key := collections.Join(claimed, peerID)
	c, err := k.PendingIdChallenges.Get(ctx, key)
	if err != nil {
		return
	}
	if err := k.PendingIdChallenges.Remove(ctx, key); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removePendingChallenge: PendingIdChallenges.Remove failed", "err", err)
	}
	if err := k.PendingIdChallengesByPeer.Remove(ctx, collections.Join(peerID, claimed)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removePendingChallenge: PendingIdChallengesByPeer.Remove failed", "err", err)
	}
	if err := k.PendingIdChallengeExp.Remove(ctx, collections.Join3(c.ExpiresAt, claimed, peerID)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removePendingChallenge: PendingIdChallengeExp.Remove failed", "err", err)
	}
	n, getErr := k.PendingIdChallengeCount.Get(ctx, peerID)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removePendingChallenge: PendingIdChallengeCount.Get failed", "err", getErr)
	}
	if n > 1 {
		if err := k.PendingIdChallengeCount.Set(ctx, peerID, n-1); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("removePendingChallenge: PendingIdChallengeCount.Set failed", "err", err)
		}
	} else {
		if err := k.PendingIdChallengeCount.Remove(ctx, peerID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("removePendingChallenge: PendingIdChallengeCount.Remove failed", "err", err)
		}
	}
}

// pendingChallengeCount returns how many challenges peerID has pending.
func (k Keeper) pendingChallengeCount(ctx context.Context, peerID string) uint64 {
	n, err := k.PendingIdChallengeCount.Get(ctx, peerID)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return 0
	}
	return n
}

// pruneExpiredIdentityChallenges is EndBlocker Phase 4. It range-walks the
// expiry queue up to now, so its cost is the number of challenges that
// actually expired, not the number pending.
func (k Keeper) pruneExpiredIdentityChallenges(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Triple[int64, string, string]]).
		EndExclusive(collections.Join3(now+1, "", ""))

	var due []collections.Triple[int64, string, string]
	err := k.PendingIdChallengeExp.Walk(ctx, rng, func(key collections.Triple[int64, string, string]) (bool, error) {
		if pruned+uint64(len(due)) >= maxPrune {
			return true, nil
		}
		due = append(due, key)
		return false, nil
	})
	for _, key := range due {
		claimed, peerID := key.K2(), key.K3()
		// The entry may be stale (the challenge was replaced with a later
		// expiry); only a challenge that really expired at this key goes.
		if c, cerr := k.PendingIdChallenges.Get(ctx, collections.Join(claimed, peerID)); cerr == nil && c.ExpiresAt == key.K1() {
			k.removePendingChallenge(ctx, claimed, peerID)
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityChallengeExpired,
				sdk.NewAttribute(types.AttributeKeyLocalAddress, claimed),
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID)))
		} else {
			if err := k.PendingIdChallengeExp.Remove(ctx, key); err != nil {
				sdkCtx.Logger().Error("pruneExpiredIdentityChallenges: PendingIdChallengeExp.Remove failed", "err", err)
			}
		}
		pruned++
	}
	return pruned, err
}

// removePeerPendingChallenges drops every pending challenge received from
// peerID, walking only that peer's index. Reports whether the set is fully
// drained within the prune budget.
func (k Keeper) removePeerPendingChallenges(ctx context.Context, peerID string, maxPrune uint64, pruned *uint64) bool {
	done := true
	var claimed []string
	rng := collections.NewPrefixedPairRange[string, string](peerID)
	if err := k.PendingIdChallengesByPeer.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		if *pruned+uint64(len(claimed)) >= maxPrune {
			done = false
			return true, nil
		}
		claimed = append(claimed, key.K2())
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removePeerPendingChallenges: PendingIdChallengesByPeer.Walk failed", "err", err)
	}
	for _, c := range claimed {
		k.removePendingChallenge(ctx, c, peerID)
		*pruned++
	}
	return done
}

// rebuildPendingChallengeIndexes re-derives the expiry, per-peer and count
// indexes from PendingIdChallenges. Used by InitGenesis. Collects first and
// writes after the walk, so the iterator never sees its own writes.
func (k Keeper) rebuildPendingChallengeIndexes(ctx context.Context) error {
	var all []types.PendingIdentityChallenge
	if err := k.PendingIdChallenges.Walk(ctx, nil, func(_ collections.Pair[string, string], c types.PendingIdentityChallenge) (bool, error) {
		all = append(all, c)
		return false, nil
	}); err != nil {
		return err
	}
	counts := map[string]uint64{}
	var peers []string
	for _, c := range all {
		peerID := c.ClaimantChainPeerId
		if _, seen := counts[peerID]; !seen {
			peers = append(peers, peerID)
		}
		counts[peerID]++
		if err := k.PendingIdChallengesByPeer.Set(ctx, collections.Join(peerID, c.ClaimedAddress)); err != nil {
			return err
		}
		if err := k.PendingIdChallengeExp.Set(ctx, collections.Join3(c.ExpiresAt, c.ClaimedAddress, peerID)); err != nil {
			return err
		}
	}
	for _, p := range peers {
		if err := k.PendingIdChallengeCount.Set(ctx, p, counts[p]); err != nil {
			return err
		}
	}
	return nil
}
