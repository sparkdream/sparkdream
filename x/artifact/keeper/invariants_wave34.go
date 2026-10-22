package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Wave 3 and 4 invariants (docs/x-artifact-spec.md §13, items 13-16).

// EditionInvariant: every edition's counters match its balances and
// pending mints, the cap holds, every balance holds units, and every
// balance's locked units equal its open listings plus outgoing pending
// transfers.
func EditionInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		type tally struct{ units, holders, reserved uint64 }
		tallies := map[[2]uint64]*tally{}
		get := func(c, e uint64) *tally {
			key := [2]uint64{c, e}
			if tallies[key] == nil {
				tallies[key] = &tally{}
			}
			return tallies[key]
		}
		lockedWant := map[BalanceKeyT]uint64{}
		_ = k.UnitListings.Walk(ctx, nil, func(_ uint64, l types.UnitListing) (bool, error) {
			lockedWant[BalanceKeyT{l.ClassId, l.EditionId, l.Seller}] += l.Quantity
			return false, nil
		})
		_ = k.PendingUnits.Walk(ctx, nil, func(_ uint64, pu types.PendingUnits) (bool, error) {
			if pu.Kind == types.InboxKind_INBOX_KIND_UNIT_TRANSFER {
				lockedWant[BalanceKeyT{pu.ClassId, pu.EditionId, pu.From}] += pu.Amount
			} else {
				get(pu.ClassId, pu.EditionId).reserved += pu.Amount
			}
			return false, nil
		})
		_ = k.Balances.Walk(ctx, nil, func(key BalanceKey, b types.Balance) (bool, error) {
			if b.Units() == 0 {
				problems = append(problems, fmt.Sprintf("empty balance %d/%d %s", b.ClassId, b.EditionId, b.Owner))
			}
			if has, _ := k.BalancesByOwner.Has(ctx, collections.Join3(b.Owner, b.ClassId, b.EditionId)); !has {
				problems = append(problems, fmt.Sprintf("balance %d/%d %s missing owner index", b.ClassId, b.EditionId, b.Owner))
			}
			if want := lockedWant[BalanceKeyT{b.ClassId, b.EditionId, b.Owner}]; want != b.Locked {
				problems = append(problems, fmt.Sprintf("balance %d/%d %s locked %d != %d listed or pending", b.ClassId, b.EditionId, b.Owner, b.Locked, want))
			}
			delete(lockedWant, BalanceKeyT{b.ClassId, b.EditionId, b.Owner})
			t := get(b.ClassId, b.EditionId)
			t.units += b.Units()
			t.holders++
			return false, nil
		})
		for key, v := range lockedWant {
			if v > 0 {
				problems = append(problems, fmt.Sprintf("%d units locked for %d/%d %s without a balance", v, key.Class, key.Edition, key.Owner))
			}
		}
		editions := map[uint64]uint64{}
		_ = k.Editions.Walk(ctx, nil, func(_ U64Pair, e types.Edition) (bool, error) {
			editions[e.ClassId]++
			t := get(e.ClassId, e.Id)
			if t.units != e.Supply || t.holders != e.Holders || t.reserved != e.Reserved {
				problems = append(problems, fmt.Sprintf("edition %d/%d supply/holders/reserved %d/%d/%d != %d/%d/%d",
					e.ClassId, e.Id, e.Supply, e.Holders, e.Reserved, t.units, t.holders, t.reserved))
			}
			if e.MaxSupply > 0 && issuedUnits(e) > e.MaxSupply {
				problems = append(problems, fmt.Sprintf("edition %d/%d issued %d > max %d", e.ClassId, e.Id, issuedUnits(e), e.MaxSupply))
			}
			delete(tallies, [2]uint64{e.ClassId, e.Id})
			return false, nil
		})
		for key := range tallies {
			problems = append(problems, fmt.Sprintf("balances or pending units for missing edition %d/%d", key[0], key[1]))
		}
		_ = k.Classes.Walk(ctx, nil, func(id uint64, c types.Class) (bool, error) {
			if c.IsEditions() && editions[id] != c.Supply {
				problems = append(problems, fmt.Sprintf("editions class %d supply %d != %d editions", id, c.Supply, editions[id]))
			}
			if !c.IsEditions() && editions[id] > 0 {
				problems = append(problems, fmt.Sprintf("unique class %d holds editions", id))
			}
			return false, nil
		})
		return report("editions", problems)
	}
}

// BalanceKeyT is a comparable (class, edition, owner) map key.
type BalanceKeyT struct {
	Class, Edition uint64
	Owner          string
}

// TreeInvariant: RENTED ⇔ an active rental naming the token's owner;
// NESTED ⇔ a parent link with its Children entry and the parent's owner;
// every token's descendant count equals its tree; every rental offer has
// its token.
func TreeInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		tokens := map[[2]uint64]types.Token{}
		_ = k.Tokens.Walk(ctx, nil, func(_ U64Pair, t types.Token) (bool, error) {
			tokens[[2]uint64{t.ClassId, t.Id}] = t
			return false, nil
		})
		children := map[[2]uint64][][2]uint64{}
		_ = k.Children.Walk(ctx, nil, func(key ChildKey) (bool, error) {
			p := [2]uint64{key.K1().K1(), key.K1().K2()}
			c := [2]uint64{key.K2().K1(), key.K2().K2()}
			children[p] = append(children[p], c)
			child, ok := tokens[c]
			if !ok || !child.IsNested() || child.Parent.ClassId != p[0] || child.Parent.TokenId != p[1] {
				problems = append(problems, fmt.Sprintf("stale child link %d/%d -> %d/%d", p[0], p[1], c[0], c[1]))
			}
			return false, nil
		})
		var count func(key [2]uint64, depth int) uint32
		count = func(key [2]uint64, depth int) uint32 {
			if depth > int(types.HardMaxNestDepth)+1 {
				problems = append(problems, fmt.Sprintf("tree under %d/%d deeper than the hard cap", key[0], key[1]))
				return 0
			}
			var n uint32
			for _, c := range children[key] {
				n += 1 + count(c, depth+1)
			}
			return n
		}
		for key, t := range tokens {
			if got := count(key, 0); got != t.Descendants {
				problems = append(problems, fmt.Sprintf("token %d/%d descendants %d != %d", key[0], key[1], t.Descendants, got))
			}
			if t.IsNested() != (t.Lock == types.TokenLock_TOKEN_LOCK_NESTED) {
				problems = append(problems, fmt.Sprintf("token %d/%d lock %s with parent %v", key[0], key[1], t.Lock, t.Parent))
			}
			if t.IsNested() {
				parent, ok := tokens[[2]uint64{t.Parent.ClassId, t.Parent.TokenId}]
				if !ok || parent.Owner != t.Owner {
					problems = append(problems, fmt.Sprintf("token %d/%d parent missing or owned by someone else", key[0], key[1]))
				}
				if has, _ := k.Children.Has(ctx, collections.Join(collections.Join(t.Parent.ClassId, t.Parent.TokenId), collections.Join(t.ClassId, t.Id))); !has {
					problems = append(problems, fmt.Sprintf("token %d/%d missing child link", key[0], key[1]))
				}
			}
			r, err := k.Rentals.Get(ctx, collections.Join(t.ClassId, t.Id))
			rented := t.Lock == types.TokenLock_TOKEN_LOCK_RENTED
			if rented != (err == nil) {
				problems = append(problems, fmt.Sprintf("token %d/%d lock %s, rental present %t", key[0], key[1], t.Lock, err == nil))
			} else if rented && r.Owner != t.Owner {
				problems = append(problems, fmt.Sprintf("rental %d/%d owner != token owner", key[0], key[1]))
			}
		}
		_ = k.RentalOffers.Walk(ctx, nil, func(key U64Pair, _ types.RentalOffer) (bool, error) {
			if _, ok := tokens[[2]uint64{key.K1(), key.K2()}]; !ok {
				problems = append(problems, fmt.Sprintf("rental offer %d/%d without token", key.K1(), key.K2()))
			}
			return false, nil
		})
		_ = k.RentalsByUser.Walk(ctx, nil, func(key AddrTokenKey) (bool, error) {
			r, err := k.Rentals.Get(ctx, collections.Join(key.K2(), key.K3()))
			if err != nil || r.User != key.K1() {
				problems = append(problems, fmt.Sprintf("stale rental index %s %d/%d", key.K1(), key.K2(), key.K3()))
			}
			return false, nil
		})
		return report("trees-and-rentals", problems)
	}
}

// HarbergerInvariant: Harberger state appears exactly on Harberger
// classes, a grant implies a due date and its HarbergerDue entry, and no
// due entry is stale.
func HarbergerInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		classes := map[uint64]types.Class{}
		_ = k.Classes.Walk(ctx, nil, func(id uint64, c types.Class) (bool, error) {
			classes[id] = c
			if err := c.Harberger.Validate(); err != nil {
				problems = append(problems, fmt.Sprintf("class %d: %v", id, err))
			}
			return false, nil
		})
		_ = k.Tokens.Walk(ctx, nil, func(_ U64Pair, t types.Token) (bool, error) {
			c := classes[t.ClassId]
			if c.IsHarberger() != (t.Harberger != nil) {
				problems = append(problems, fmt.Sprintf("token %d/%d harberger state mismatch", t.ClassId, t.Id))
				return false, nil
			}
			if t.Harberger == nil {
				return false, nil
			}
			h := t.Harberger
			if (h.GrantId == 0) != (h.NextDue == 0) {
				problems = append(problems, fmt.Sprintf("token %d/%d grant %d next_due %d", t.ClassId, t.Id, h.GrantId, h.NextDue))
			}
			if h.NextDue != 0 {
				if has, _ := k.HarbergerDue.Has(ctx, collections.Join3(h.NextDue, t.ClassId, t.Id)); !has {
					problems = append(problems, fmt.Sprintf("token %d/%d missing harberger due entry", t.ClassId, t.Id))
				}
			}
			return false, nil
		})
		_ = k.HarbergerDue.Walk(ctx, nil, func(key TimeTokenKey) (bool, error) {
			t, err := k.Tokens.Get(ctx, collections.Join(key.K2(), key.K3()))
			if err != nil || t.Harberger == nil || t.Harberger.NextDue != key.K1() {
				problems = append(problems, fmt.Sprintf("stale harberger due entry %d %d/%d", key.K1(), key.K2(), key.K3()))
			}
			return false, nil
		})
		return report("harberger", problems)
	}
}

// editionEscrow is what the module holds for editions: balance deposits,
// pending-unit escrows and every holder's unclaimed or accrued rewards.
func (k Keeper) editionEscrow(ctx sdk.Context) math.Int {
	need := math.ZeroInt()
	rpu := map[[2]uint64]math.Int{}
	_ = k.Editions.Walk(ctx, nil, func(_ U64Pair, e types.Edition) (bool, error) {
		rpu[[2]uint64{e.ClassId, e.Id}] = e.RewardPerUnit
		return false, nil
	})
	_ = k.Balances.Walk(ctx, nil, func(_ BalanceKey, b types.Balance) (bool, error) {
		need = need.Add(b.Deposit)
		if r, ok := rpu[[2]uint64{b.ClassId, b.EditionId}]; ok {
			need = need.Add(b.Claimable(r))
		}
		return false, nil
	})
	_ = k.PendingUnits.Walk(ctx, nil, func(_ uint64, pu types.PendingUnits) (bool, error) {
		need = need.Add(pu.Deposit)
		return false, nil
	})
	_ = k.Offers.Walk(ctx, nil, func(_ uint64, o types.Offer) (bool, error) {
		need = need.Add(o.Price.Amount)
		return false, nil
	})
	_ = k.Auctions.Walk(ctx, nil, func(_ uint64, a types.Auction) (bool, error) {
		if a.HighBidder != "" {
			need = need.Add(a.HighBid)
		}
		return false, nil
	})
	return need
}

// MarketEscrowInvariant: AUCTION ⇔ an auction of the token by its owner,
// every auction and offer index points at its record, and no high bid
// sits below the reserve.
func MarketEscrowInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		_ = k.Tokens.Walk(ctx, nil, func(key U64Pair, t types.Token) (bool, error) {
			id, err := k.AuctionByToken.Get(ctx, key)
			if (t.Lock == types.TokenLock_TOKEN_LOCK_AUCTION) != (err == nil) {
				problems = append(problems, fmt.Sprintf("token %d/%d lock %s, auction present %t", t.ClassId, t.Id, t.Lock, err == nil))
				return false, nil
			}
			if err == nil {
				if a, err := k.Auctions.Get(ctx, id); err != nil || a.Seller != t.Owner {
					problems = append(problems, fmt.Sprintf("auction %d of %d/%d missing or not by the owner", id, t.ClassId, t.Id))
				}
			}
			return false, nil
		})
		_ = k.Auctions.Walk(ctx, nil, func(id uint64, a types.Auction) (bool, error) {
			if got, err := k.AuctionByToken.Get(ctx, collections.Join(a.ClassId, a.TokenId)); err != nil || got != id {
				problems = append(problems, fmt.Sprintf("auction %d missing its token index", id))
			}
			if has, _ := k.AuctionEnds.Has(ctx, collections.Join(a.EndsAt, id)); !has {
				problems = append(problems, fmt.Sprintf("auction %d missing its end index", id))
			}
			if a.HighBidder != "" && a.HighBid.LT(a.ReservePrice.Amount) {
				problems = append(problems, fmt.Sprintf("auction %d high bid below reserve", id))
			}
			return false, nil
		})
		_ = k.Offers.Walk(ctx, nil, func(id uint64, o types.Offer) (bool, error) {
			if has, _ := k.OffersByBidder.Has(ctx, collections.Join(o.Bidder, id)); !has {
				problems = append(problems, fmt.Sprintf("offer %d missing its bidder index", id))
			}
			if has, _ := k.OffersByToken.Has(ctx, collections.Join3(o.ClassId, o.TokenId, id)); !has {
				problems = append(problems, fmt.Sprintf("offer %d missing its token index", id))
			}
			return false, nil
		})
		return report("market-escrow", problems)
	}
}
