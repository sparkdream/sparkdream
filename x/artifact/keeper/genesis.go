package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
// Derived indexes and counters are rebuilt, never imported.
func (k Keeper) InitGenesis(ctx context.Context, gs types.GenesisState) error {
	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}
	if err := k.ClassSeq.Set(ctx, gs.ClassSeq); err != nil {
		return err
	}
	if err := k.HideSeq.Set(ctx, gs.HideSeq); err != nil {
		return err
	}
	for _, c := range gs.Classes {
		if err := k.Classes.Set(ctx, c.Id, c); err != nil {
			return err
		}
		if err := k.ClassesByOwner.Set(ctx, collections.Join(c.Owner, c.Id)); err != nil {
			return err
		}
		if err := k.adjustCreatorCount(ctx, c.Creator, 1); err != nil {
			return err
		}
	}
	for _, t := range gs.Tokens {
		if err := k.Tokens.Set(ctx, collections.Join(t.ClassId, t.Id), t); err != nil {
			return err
		}
		if err := k.TokensByOwner.Set(ctx, collections.Join3(t.Owner, t.ClassId, t.Id)); err != nil {
			return err
		}
	}
	for _, e := range gs.PublicMintCounts {
		if err := k.PublicMintCount.Set(ctx, collections.Join(e.ClassId, e.Address), uint64(e.Count)); err != nil {
			return err
		}
	}
	for _, e := range gs.ReceivePolicies {
		if err := k.ReceivePolicies.Set(ctx, e.Address, uint64(e.Policy)); err != nil {
			return err
		}
	}
	bump := func(origin, to string) error {
		pk := collections.Join(origin, to)
		pair, _ := k.PendingPairCount.Get(ctx, pk)
		if err := k.PendingPairCount.Set(ctx, pk, pair+1); err != nil {
			return err
		}
		total, _ := k.InboxCount.Get(ctx, to)
		return k.InboxCount.Set(ctx, to, total+1)
	}
	for _, pt := range gs.PendingTransfers {
		if err := k.PendingTransfers.Set(ctx, collections.Join(pt.ClassId, pt.TokenId), pt); err != nil {
			return err
		}
		if err := k.indexPending(ctx, types.InboxKind_INBOX_KIND_TRANSFER, pt.ClassId, pt.TokenId, pt.From, pt.To, pt.ExpiresAt); err != nil {
			return err
		}
		if err := bump(pt.From, pt.To); err != nil {
			return err
		}
	}
	for _, pm := range gs.PendingMints {
		if err := k.PendingMints.Set(ctx, collections.Join(pm.ClassId, pm.TokenId), pm); err != nil {
			return err
		}
		if err := k.indexPending(ctx, types.InboxKind_INBOX_KIND_MINT, pm.ClassId, pm.TokenId, pm.Minter, pm.To, pm.ExpiresAt); err != nil {
			return err
		}
		if err := bump(types.MintOrigin(pm.ClassId), pm.To); err != nil {
			return err
		}
	}
	for _, l := range gs.Listings {
		if err := k.Listings.Set(ctx, collections.Join(l.ClassId, l.TokenId), l); err != nil {
			return err
		}
		if err := k.ListingsBySeller.Set(ctx, collections.Join3(l.Seller, l.ClassId, l.TokenId)); err != nil {
			return err
		}
		if err := k.ListingExpiry.Set(ctx, collections.Join3(l.ExpiresAt, l.ClassId, l.TokenId)); err != nil {
			return err
		}
	}
	for _, po := range gs.PendingClassOwners {
		if err := k.PendingClassOwners.Set(ctx, po.ClassId, po); err != nil {
			return err
		}
		if err := k.PendingOwnerExpiry.Set(ctx, collections.Join(po.ExpiresAt, po.ClassId)); err != nil {
			return err
		}
	}
	for _, hr := range gs.HideRecords {
		if err := k.HideRecords.Set(ctx, hr.Id, hr); err != nil {
			return err
		}
		if err := k.HidesByTargetAll.Set(ctx, collections.Join3(hr.ClassId, hr.TokenId, hr.Id)); err != nil {
			return err
		}
		if hr.Outcome == types.HideOutcome_HIDE_OUTCOME_PENDING {
			if err := k.HideByTarget.Set(ctx, targetKey(hr.ClassId, hr.TokenId), hr.Id); err != nil {
				return err
			}
			if err := k.HideExpiry.Set(ctx, collections.Join(hr.AppealDeadline, hr.Id)); err != nil {
				return err
			}
		}
	}
	for _, q := range gs.ClassCancelQueue {
		if err := k.ClassCancelQueue.Set(ctx, q.ClassId, q.Cursor); err != nil {
			return err
		}
	}
	for _, q := range gs.ClassScrubQueue {
		if err := k.ClassScrubQueue.Set(ctx, q.ClassId, q.Cursor); err != nil {
			return err
		}
	}
	for _, e := range gs.SentinelDailyHides {
		if err := k.SentinelDailyHides.Set(ctx, collections.Join(e.Sentinel, e.Day), uint64(e.Count)); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error
	gs := types.DefaultGenesis()
	if gs.Params, err = k.Params.Get(ctx); err != nil {
		return nil, err
	}
	if gs.ClassSeq, err = k.ClassSeq.Peek(ctx); err != nil {
		return nil, err
	}
	if gs.HideSeq, err = k.HideSeq.Peek(ctx); err != nil {
		return nil, err
	}
	walk := func(fn func() error) {
		if err == nil {
			err = fn()
		}
	}
	walk(func() error {
		return k.Classes.Walk(ctx, nil, func(_ uint64, c types.Class) (bool, error) {
			gs.Classes = append(gs.Classes, c)
			return false, nil
		})
	})
	walk(func() error {
		return k.Tokens.Walk(ctx, nil, func(_ U64Pair, t types.Token) (bool, error) {
			gs.Tokens = append(gs.Tokens, t)
			return false, nil
		})
	})
	walk(func() error {
		return k.PublicMintCount.Walk(ctx, nil, func(key collections.Pair[uint64, string], v uint64) (bool, error) {
			gs.PublicMintCounts = append(gs.PublicMintCounts, types.PublicMintCountEntry{ClassId: key.K1(), Address: key.K2(), Count: uint32(v)})
			return false, nil
		})
	})
	walk(func() error {
		return k.ReceivePolicies.Walk(ctx, nil, func(addr string, v uint64) (bool, error) {
			gs.ReceivePolicies = append(gs.ReceivePolicies, types.ReceivePolicyEntry{Address: addr, Policy: types.ReceivePolicy(v)})
			return false, nil
		})
	})
	walk(func() error {
		return k.PendingTransfers.Walk(ctx, nil, func(_ U64Pair, pt types.PendingTransfer) (bool, error) {
			gs.PendingTransfers = append(gs.PendingTransfers, pt)
			return false, nil
		})
	})
	walk(func() error {
		return k.PendingMints.Walk(ctx, nil, func(_ U64Pair, pm types.PendingMint) (bool, error) {
			gs.PendingMints = append(gs.PendingMints, pm)
			return false, nil
		})
	})
	walk(func() error {
		return k.Listings.Walk(ctx, nil, func(_ U64Pair, l types.Listing) (bool, error) {
			gs.Listings = append(gs.Listings, l)
			return false, nil
		})
	})
	walk(func() error {
		return k.PendingClassOwners.Walk(ctx, nil, func(_ uint64, po types.PendingClassOwner) (bool, error) {
			gs.PendingClassOwners = append(gs.PendingClassOwners, po)
			return false, nil
		})
	})
	walk(func() error {
		return k.HideRecords.Walk(ctx, nil, func(_ uint64, hr types.HideRecord) (bool, error) {
			gs.HideRecords = append(gs.HideRecords, hr)
			return false, nil
		})
	})
	walk(func() error {
		return k.ClassCancelQueue.Walk(ctx, nil, func(id, cursor uint64) (bool, error) {
			gs.ClassCancelQueue = append(gs.ClassCancelQueue, types.QueueCursor{ClassId: id, Cursor: cursor})
			return false, nil
		})
	})
	walk(func() error {
		return k.ClassScrubQueue.Walk(ctx, nil, func(id, cursor uint64) (bool, error) {
			gs.ClassScrubQueue = append(gs.ClassScrubQueue, types.QueueCursor{ClassId: id, Cursor: cursor})
			return false, nil
		})
	})
	walk(func() error {
		return k.SentinelDailyHides.Walk(ctx, nil, func(key collections.Pair[string, int64], v uint64) (bool, error) {
			gs.SentinelDailyHides = append(gs.SentinelDailyHides, types.SentinelDailyHideEntry{Sentinel: key.K1(), Day: key.K2(), Count: uint32(v)})
			return false, nil
		})
	})
	if err != nil {
		return nil, err
	}
	return gs, nil
}
