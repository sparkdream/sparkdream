package keeper

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
	commontypes "sparkdream/x/common/types"
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
	// Media labels are recomputed under the current rules rather than
	// trusted from the file (docs/content-scanning.md).
	for _, c := range gs.Classes {
		c.MediaFlags, c.MediaRulesVersion = types.LabelClass(c), commontypes.MediaRulesVersion
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
		t.MediaFlags, t.MediaRulesVersion = types.LabelTokenAll(t), commontypes.MediaRulesVersion
		if err := k.Tokens.Set(ctx, collections.Join(t.ClassId, t.Id), t); err != nil {
			return err
		}
		if err := k.TokensByOwner.Set(ctx, collections.Join3(t.Owner, t.ClassId, t.Id)); err != nil {
			return err
		}
		if c, err := k.Classes.Get(ctx, t.ClassId); err == nil {
			if err := k.indexTokenExpiry(ctx, c, t); err != nil {
				return err
			}
		}
		if t.IsNested() {
			if err := k.Children.Set(ctx, collections.Join(refKey(*t.Parent), refKey(tokenRef(t)))); err != nil {
				return err
			}
		}
		if err := k.indexHarbergerDue(ctx, t); err != nil {
			return err
		}
	}
	for _, mp := range gs.MediaProposals {
		if err := k.MediaProposals.Set(ctx, collections.Join3(mp.ClassId, mp.TokenId, mp.Id), mp); err != nil {
			return err
		}
	}
	for _, q := range gs.ClassReleaseQueue {
		if err := k.ClassReleaseQueue.Set(ctx, q.ClassId, q.Cursor); err != nil {
			return err
		}
	}
	for _, e := range gs.PublicMintCounts {
		if err := k.PublicMintCount.Set(ctx, collections.Join(e.ClassId, e.Address), uint64(e.Count)); err != nil {
			return err
		}
	}
	for _, e := range gs.ReceivePolicies {
		if err := k.ReceivePolicies.Set(ctx, e.Address, commontypes.Saturate[uint64](e.Policy)); err != nil {
			return err
		}
	}
	bump := func(origin, to string) error {
		pk := collections.Join(origin, to)
		pair, getErr := k.PendingPairCount.Get(ctx, pk)
		if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
			sdk.UnwrapSDKContext(ctx).Logger().Error("InitGenesis: PendingPairCount.Get failed", "err", getErr)
		}
		if err := k.PendingPairCount.Set(ctx, pk, pair+1); err != nil {
			return err
		}
		total, getErr := k.InboxCount.Get(ctx, to)
		if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
			sdk.UnwrapSDKContext(ctx).Logger().Error("InitGenesis: InboxCount.Get failed", "err", getErr)
		}
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
	return k.initWave34Genesis(ctx, gs, bump)
}

// initWave34Genesis imports editions, balances, pending units, unit
// listings, recipes, rentals and rental offers, rebuilding their indexes
// and the inbox counters.
func (k Keeper) initWave34Genesis(ctx context.Context, gs types.GenesisState, bump func(origin, to string) error) error {
	for _, e := range gs.Editions {
		e.MediaFlags, e.MediaRulesVersion = types.LabelToken(e.Metadata), commontypes.MediaRulesVersion
		if err := k.Editions.Set(ctx, collections.Join(e.ClassId, e.Id), e); err != nil {
			return err
		}
		if e.Claim.InitiativeId != 0 {
			if err := k.CertificatesByInitiative.Set(ctx, collections.Join3(e.Claim.InitiativeId, e.ClassId, e.Id)); err != nil {
				return err
			}
		}
	}
	for _, b := range gs.Balances {
		if err := k.Balances.Set(ctx, balanceKey(b.ClassId, b.EditionId, b.Owner), b); err != nil {
			return err
		}
		if err := k.BalancesByOwner.Set(ctx, collections.Join3(b.Owner, b.ClassId, b.EditionId)); err != nil {
			return err
		}
	}
	if err := k.PendingUnitSeq.Set(ctx, gs.PendingUnitSeq); err != nil {
		return err
	}
	for _, pu := range gs.PendingUnits {
		if err := k.PendingUnits.Set(ctx, pu.Id, pu); err != nil {
			return err
		}
		if err := k.indexPending(ctx, pu.Kind, unitNamespace, pu.Id, pu.From, pu.To, pu.ExpiresAt); err != nil {
			return err
		}
		if err := bump(unitOriginOf(pu), pu.To); err != nil {
			return err
		}
	}
	if err := k.UnitListingSeq.Set(ctx, gs.UnitListingSeq); err != nil {
		return err
	}
	for _, l := range gs.UnitListings {
		if err := k.UnitListings.Set(ctx, l.Id, l); err != nil {
			return err
		}
		if err := k.UnitListingsByEdition.Set(ctx, collections.Join3(l.ClassId, l.EditionId, l.Id)); err != nil {
			return err
		}
		if err := k.UnitListingsBySeller.Set(ctx, collections.Join(l.Seller, l.Id)); err != nil {
			return err
		}
		if err := k.UnitListingExpiry.Set(ctx, collections.Join(l.ExpiresAt, l.Id)); err != nil {
			return err
		}
	}
	for _, r := range gs.CraftRecipes {
		if err := k.CraftRecipes.Set(ctx, collections.Join(r.ClassId, r.Id), r); err != nil {
			return err
		}
	}
	for _, r := range gs.Rentals {
		if err := k.Rentals.Set(ctx, collections.Join(r.ClassId, r.TokenId), r); err != nil {
			return err
		}
		if err := k.RentalsByUser.Set(ctx, collections.Join3(r.User, r.ClassId, r.TokenId)); err != nil {
			return err
		}
		if err := k.RentalExpiry.Set(ctx, collections.Join3(r.ExpiresAt, r.ClassId, r.TokenId)); err != nil {
			return err
		}
	}
	for _, o := range gs.RentalOffers {
		if err := k.RentalOffers.Set(ctx, collections.Join(o.ClassId, o.TokenId), o); err != nil {
			return err
		}
		if err := k.RentalOfferExpiry.Set(ctx, collections.Join3(o.ExpiresAt, o.ClassId, o.TokenId)); err != nil {
			return err
		}
	}
	if err := k.OfferSeq.Set(ctx, gs.OfferSeq); err != nil {
		return err
	}
	for _, o := range gs.Offers {
		if err := k.Offers.Set(ctx, o.Id, o); err != nil {
			return err
		}
		if err := k.OffersByToken.Set(ctx, collections.Join3(o.ClassId, o.TokenId, o.Id)); err != nil {
			return err
		}
		if err := k.OffersByBidder.Set(ctx, collections.Join(o.Bidder, o.Id)); err != nil {
			return err
		}
		if err := k.OfferExpiry.Set(ctx, collections.Join(o.ExpiresAt, o.Id)); err != nil {
			return err
		}
	}
	if err := k.AuctionSeq.Set(ctx, gs.AuctionSeq); err != nil {
		return err
	}
	for _, a := range gs.Auctions {
		if err := k.Auctions.Set(ctx, a.Id, a); err != nil {
			return err
		}
		if err := k.AuctionByToken.Set(ctx, collections.Join(a.ClassId, a.TokenId), a.Id); err != nil {
			return err
		}
		if err := k.AuctionEnds.Set(ctx, collections.Join(a.EndsAt, a.Id)); err != nil {
			return err
		}
		if err := k.AuctionsBySeller.Set(ctx, collections.Join(a.Seller, a.Id)); err != nil {
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
	// walk runs fn only while nothing has failed; every step after the
	// first walk (sequence peeks included) goes through it, so an earlier
	// error is never overwritten.
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
			gs.PublicMintCounts = append(gs.PublicMintCounts, types.PublicMintCountEntry{ClassId: key.K1(), Address: key.K2(), Count: commontypes.Saturate[uint32](v)})
			return false, nil
		})
	})
	walk(func() error {
		return k.ReceivePolicies.Walk(ctx, nil, func(addr string, v uint64) (bool, error) {
			gs.ReceivePolicies = append(gs.ReceivePolicies, types.ReceivePolicyEntry{Address: addr, Policy: commontypes.Saturate[types.ReceivePolicy](v)})
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
			gs.SentinelDailyHides = append(gs.SentinelDailyHides, types.SentinelDailyHideEntry{Sentinel: key.K1(), Day: key.K2(), Count: commontypes.Saturate[uint32](v)})
			return false, nil
		})
	})
	walk(func() error {
		return k.MediaProposals.Walk(ctx, nil, func(_ collections.Triple[uint64, uint64, uint64], mp types.MediaProposal) (bool, error) {
			gs.MediaProposals = append(gs.MediaProposals, mp)
			return false, nil
		})
	})
	walk(func() error {
		return k.ClassReleaseQueue.Walk(ctx, nil, func(id, cursor uint64) (bool, error) {
			gs.ClassReleaseQueue = append(gs.ClassReleaseQueue, types.QueueCursor{ClassId: id, Cursor: cursor})
			return false, nil
		})
	})
	walk(func() (e error) { gs.PendingUnitSeq, e = k.PendingUnitSeq.Peek(ctx); return e })
	walk(func() (e error) { gs.UnitListingSeq, e = k.UnitListingSeq.Peek(ctx); return e })
	walk(func() error {
		return k.Editions.Walk(ctx, nil, func(_ U64Pair, e types.Edition) (bool, error) {
			gs.Editions = append(gs.Editions, e)
			return false, nil
		})
	})
	walk(func() error {
		return k.Balances.Walk(ctx, nil, func(_ BalanceKey, b types.Balance) (bool, error) {
			gs.Balances = append(gs.Balances, b)
			return false, nil
		})
	})
	walk(func() error {
		return k.PendingUnits.Walk(ctx, nil, func(_ uint64, pu types.PendingUnits) (bool, error) {
			gs.PendingUnits = append(gs.PendingUnits, pu)
			return false, nil
		})
	})
	walk(func() error {
		return k.UnitListings.Walk(ctx, nil, func(_ uint64, l types.UnitListing) (bool, error) {
			gs.UnitListings = append(gs.UnitListings, l)
			return false, nil
		})
	})
	walk(func() error {
		return k.CraftRecipes.Walk(ctx, nil, func(_ U64Pair, r types.CraftRecipe) (bool, error) {
			gs.CraftRecipes = append(gs.CraftRecipes, r)
			return false, nil
		})
	})
	walk(func() error {
		return k.Rentals.Walk(ctx, nil, func(_ U64Pair, r types.Rental) (bool, error) {
			gs.Rentals = append(gs.Rentals, r)
			return false, nil
		})
	})
	walk(func() error {
		return k.RentalOffers.Walk(ctx, nil, func(_ U64Pair, o types.RentalOffer) (bool, error) {
			gs.RentalOffers = append(gs.RentalOffers, o)
			return false, nil
		})
	})
	walk(func() (e error) { gs.OfferSeq, e = k.OfferSeq.Peek(ctx); return e })
	walk(func() (e error) { gs.AuctionSeq, e = k.AuctionSeq.Peek(ctx); return e })
	walk(func() error {
		return k.Offers.Walk(ctx, nil, func(_ uint64, o types.Offer) (bool, error) {
			gs.Offers = append(gs.Offers, o)
			return false, nil
		})
	})
	walk(func() error {
		return k.Auctions.Walk(ctx, nil, func(_ uint64, a types.Auction) (bool, error) {
			gs.Auctions = append(gs.Auctions, a)
			return false, nil
		})
	})
	if err != nil {
		return nil, err
	}
	return gs, nil
}
