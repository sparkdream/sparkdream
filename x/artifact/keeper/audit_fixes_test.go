package keeper_test

import (
	"encoding/hex"
	"errors"
	"testing"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

// Regression tests for ART-16 .. ART-21 (security review 2026-10-26).

// hasEvent reports whether the context's event manager saw an event type.
func hasEvent(ctx sdk.Context, typ string) bool {
	for _, e := range ctx.EventManager().Events() {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// ART-16: an auction of an expiring token ends at least auction_extension
// seconds before the expiry, the anti-sniping extension is capped the same
// way, and a late bid therefore still settles into a sale.
func TestAuctionNeverOutlivesTokenExpiry(t *testing.T) {
	f := initFixture(t)
	f.setParams(func(p *types.Params) { p.AuctionExtension = 60 })
	ext := f.params().AuctionExtension
	classID := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Expiry = types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_MARK, Term: 10 * ext, RenewalPrice: spark(1)}
	})
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	exp := f.token(classID, tok).ExpiresAt
	room := exp - f.ctx.BlockTime().Unix() - ext

	start := func(dur int64) (uint64, error) {
		r, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.bob.String(), ClassId: classID, TokenId: tok,
			ReservePrice: spark(2), MinIncrementBps: 1000, Duration: dur})
		if err != nil {
			return 0, err
		}
		return r.AuctionId, nil
	}
	// Ending at the expiry (previously accepted) or inside the margin is refused.
	_, err := start(exp - f.ctx.BlockTime().Unix())
	require.ErrorIs(t, err, types.ErrTokenExpired)
	_, err = start(room + 1)
	require.ErrorIs(t, err, types.ErrTokenExpired)
	id, err := start(room)
	require.NoError(t, err)

	// A bid in the last seconds cannot push the end towards the expiry.
	f.advance(room-ext+2, 1)
	r, err := f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.carol.String(), AuctionId: id, Amount: spark(2)})
	require.NoError(t, err)
	require.LessOrEqual(t, r.EndsAt, exp-ext, "extension capped auction_extension seconds before expiry")

	// The auction settles a live token: carol buys it.
	f.advance(r.EndsAt-f.ctx.BlockTime().Unix(), 1)
	f.endBlock()
	got := f.token(classID, tok)
	require.Equal(t, f.carol.String(), got.Owner, "the late bid wins instead of the auction settling unsold")
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, got.Lock)
	require.False(t, types.IsExpired(got, f.ctx.BlockTime().Unix()))
}

// ART-16: the extension is still granted, partially, when it fits.
func TestAuctionExtensionPartiallyGranted(t *testing.T) {
	f := initFixture(t)
	f.setParams(func(p *types.Params) { p.AuctionExtension = 60 })
	ext := f.params().AuctionExtension
	classID := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Expiry = types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_BURN, Term: 10 * ext}
	})
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	exp := f.token(classID, tok).ExpiresAt
	dur := exp - f.ctx.BlockTime().Unix() - ext - 10 // 10s of extension room past ends_at
	resp, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.bob.String(), ClassId: classID, TokenId: tok,
		ReservePrice: spark(2), Duration: dur})
	require.NoError(t, err)
	f.advance(dur-1, 1)
	r, err := f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.carol.String(), AuctionId: resp.AuctionId, Amount: spark(2)})
	require.NoError(t, err)
	require.Equal(t, exp-ext, r.EndsAt, "extended by 10s, not by auction_extension")
	f.advance(r.EndsAt-f.ctx.BlockTime().Unix(), 1)
	f.endBlock()
	require.Equal(t, f.carol.String(), f.token(classID, tok).Owner, "BURN token sold before it is destroyed")
}

// ART-17: a failing release visit no longer deletes the class cursor. The
// first failure holds the cursor for a retry; once the fault is repaired the
// whole class is released without a second seed reveal.
func TestReleaseQueueSurvivesFailedVisit(t *testing.T) {
	f := initFixture(t)
	seed := []byte("aurora-seed")
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(1)}
		m.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: types.SeedCommitment(seed),
			Deadline: f.ctx.BlockTime().Unix() + types.MinRevealWindow + 100}
		m.PayoutAddress = f.carol.String()
	})
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 3, MaxPrice: spark(1)})
	require.NoError(t, err)
	_, err = f.msg.RevealSeed(f.ctx, &types.MsgRevealSeed{Owner: f.alice.String(), ClassId: id, Seed: hex.EncodeToString(seed)})
	require.NoError(t, err)
	f.setParams(func(p *types.Params) { p.MaxExpirationsPerBlock = 10 })

	carolB := f.bank.bal(f.carol)
	f.bank.failTo["community_pool"] = true // the sale fee cannot be paid
	f.advance(1, 1)
	f.endBlock()
	cursor, err := f.keeper.ClassReleaseQueue.Get(f.ctx, id)
	require.NoError(t, err, "the queue entry survives the failure")
	require.Equal(t, uint64(1), cursor, "cursor held at the failing token")
	failKey := collections.Join("release", u64s(id)+"/1")
	has, err := f.keeper.FailedExpiry.Has(f.ctx, failKey)
	require.NoError(t, err)
	require.True(t, has)

	// Repaired: the retry releases everything and clears the record.
	delete(f.bank.failTo, "community_pool")
	f.advance(1, 1)
	f.endBlock()
	fee := spark(1).Amount.MulRaw(int64(f.params().SaleFeeBps)).QuoRaw(10000)
	require.Equal(t, carolB.Add(spark(3).Amount.Sub(fee.MulRaw(3))), f.bank.bal(f.carol))
	_, err = f.keeper.ClassReleaseQueue.Get(f.ctx, id)
	require.Error(t, err, "queue drained")
	has, err = f.keeper.FailedExpiry.Has(f.ctx, failKey)
	require.NoError(t, err)
	require.False(t, has, "a successful retry clears the record")
}

// ART-17: a token that fails again on its retry is skipped (recorded) so a
// permanent fault cannot stall the rest of the class.
func TestReleaseQueueSkipsRepeatedFailure(t *testing.T) {
	f := initFixture(t)
	seed := []byte("zenith-seed")
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(1)}
		m.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: types.SeedCommitment(seed),
			Deadline: f.ctx.BlockTime().Unix() + types.MinRevealWindow + 100}
		m.PayoutAddress = f.carol.String()
	})
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 3, MaxPrice: spark(1)})
	require.NoError(t, err)
	_, err = f.msg.RevealSeed(f.ctx, &types.MsgRevealSeed{Owner: f.alice.String(), ClassId: id, Seed: hex.EncodeToString(seed)})
	require.NoError(t, err)
	f.setParams(func(p *types.Params) { p.MaxExpirationsPerBlock = 10 })

	f.bank.failTo["community_pool"] = true // the sale fee cannot be paid
	f.advance(1, 1)
	f.endBlock() // token 1 fails: held
	f.advance(1, 1)
	f.endBlock() // token 1 fails again: skipped; token 2 fails: held
	cursor, err := f.keeper.ClassReleaseQueue.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, uint64(2), cursor)

	delete(f.bank.failTo, "community_pool")
	f.advance(1, 1)
	f.endBlock()
	_, err = f.keeper.ClassReleaseQueue.Get(f.ctx, id)
	require.Error(t, err, "rest of the class drained")
	require.True(t, f.token(id, 1).Paid.IsPositive(), "the skipped token keeps its escrow")
	require.True(t, f.token(id, 2).Paid.IsZero())
	require.True(t, f.token(id, 3).Paid.IsZero())
	has, err := f.keeper.FailedExpiry.Has(f.ctx, collections.Join("release", u64s(id)+"/1"))
	require.NoError(t, err)
	require.True(t, has, "the skipped token stays recorded")
}

// ART-18: a failing x/rep bond release or verdict report is still
// best-effort but emits an event instead of only a log line.
func TestSentinelRepFailuresEmitEvents(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 1)
	hid, err := f.hide(f.sentinel.String(), types.HideTargetKind_HIDE_TARGET_KIND_TOKEN, id, 1)
	require.NoError(t, err)
	f.advance(10, f.params().SentinelUnhideWindowBlocks+1)

	f.rep.releaseErr = errors.New("rep store failure")
	f.rep.outcomeErr = errors.New("rep store failure")
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	_, err = f.msg.UnhideContent(f.ctx, &types.MsgUnhideContent{Authority: f.ops.String(), HideId: hid})
	require.NoError(t, err, "the hide still closes")
	require.Equal(t, types.HideOutcome_HIDE_OUTCOME_UNHIDDEN, f.hideRecord(hid).Outcome)
	require.True(t, hasEvent(f.ctx, types.EventSentinelBondReleaseFailed))
	require.True(t, hasEvent(f.ctx, types.EventSentinelOutcomeFailed))
}

// ART-19: an expired rental the EndBlocker has not ended yet is no longer
// served by Rental, RentalsByUser or the token view's user.
func TestExpiredRentalQueriesFiltered(t *testing.T) {
	f := initFixture(t)
	classID := f.createClass(f.alice)
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	offer, err := f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.bob.String(), ClassId: classID, TokenId: tok,
		Price: spark(5), Duration: 600, OfferDuration: 3600})
	require.NoError(t, err)
	_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: classID, TokenId: tok,
		ExpectedPrice: spark(5), ExpectedNonce: offer.Nonce})
	require.NoError(t, err)
	require.Equal(t, f.carol.String(), f.tokenQuery(classID, tok).User)

	f.advance(600, 1) // term over, EndBlocker not run
	_, err = f.query.Rental(f.ctx, &types.QueryRentalRequest{ClassId: classID, TokenId: tok})
	require.Error(t, err)
	list, err := f.query.RentalsByUser(f.ctx, &types.QueryRentalsByUserRequest{User: f.carol.String()})
	require.NoError(t, err)
	require.Empty(t, list.Rentals)
	require.Empty(t, f.tokenQuery(classID, tok).User, "the renter is no longer the user")
	require.Equal(t, types.TokenLock_TOKEN_LOCK_RENTED, f.token(classID, tok).Lock, "state is untouched until the EndBlocker")
}

// ART-20: signer-as-recipient handlers apply the recipient rule.
func TestSignerRecipientRule(t *testing.T) {
	f := initFixture(t)
	mod := moduleAddress()
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: mod, ClassId: 1, Quantity: 1})
	require.ErrorIs(t, err, types.ErrInvalidRecipient)
	_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: mod, ClassId: 1, TokenId: 1})
	require.ErrorIs(t, err, types.ErrInvalidRecipient)
	_, err = f.msg.Craft(f.ctx, &types.MsgCraft{Crafter: mod, ClassId: 1, RecipeId: 1})
	require.ErrorIs(t, err, types.ErrInvalidRecipient)
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: mod})
	require.ErrorIs(t, err, types.ErrInvalidRecipient)

	blocked := sdk.AccAddress([]byte("blocked-module-acct.")).String()
	f.bank.blocked[blocked] = true
	_, err = f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: blocked, ClassId: 1, Quantity: 1})
	require.ErrorIs(t, err, types.ErrInvalidRecipient)
}

// ART-21: genesis validation rejects duplicate keyed records and malformed
// auctions instead of importing counters that disagree with the records.
func TestGenesisValidateSemanticGaps(t *testing.T) {
	f := initFixture(t)
	populate(f)
	classID := f.createClass(f.alice)
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	_, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.bob.String(), ClassId: classID, TokenId: tok,
		ReservePrice: spark(2), MinIncrementBps: 500, Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.carol.String(), AuctionId: 1, Amount: spark(3)})
	require.NoError(t, err)

	base, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, base.Validate())
	require.Len(t, base.PendingMints, 1)
	require.Len(t, base.Auctions, 1)
	require.NotEmpty(t, base.Listings)

	fresh := func() *types.GenesisState {
		gs, err := f.keeper.ExportGenesis(f.ctx)
		require.NoError(t, err)
		return gs
	}
	cases := map[string]func(gs *types.GenesisState){
		"duplicate pending mint": func(gs *types.GenesisState) {
			pm := gs.PendingMints[0]
			gs.PendingMints = append(gs.PendingMints, pm)
			for i := range gs.Classes {
				if gs.Classes[i].Id == pm.ClassId {
					gs.Classes[i].ReservedSupply++
				}
			}
		},
		"duplicate listing": func(gs *types.GenesisState) { gs.Listings = append(gs.Listings, gs.Listings[0]) },
		"duplicate pending transfer": func(gs *types.GenesisState) {
			gs.PendingTransfers = append(gs.PendingTransfers, gs.PendingTransfers[0])
		},
		"nil reserve with a bidder": func(gs *types.GenesisState) {
			gs.Auctions[0].ReservePrice = sdk.Coin{Denom: bond}
		},
		"zero reserve": func(gs *types.GenesisState) { gs.Auctions[0].ReservePrice = sdk.NewInt64Coin(bond, 0) },
		"bad reserve denom": func(gs *types.GenesisState) {
			gs.Auctions[0].ReservePrice = sdk.Coin{Denom: "!", Amount: sdkmath.NewInt(5)}
		},
		"increment above 100%": func(gs *types.GenesisState) { gs.Auctions[0].MinIncrementBps = 10001 },
		"negative high bid":    func(gs *types.GenesisState) { gs.Auctions[0].HighBid = sdkmath.NewInt(-1) },
		"high bid, no bidder":  func(gs *types.GenesisState) { gs.Auctions[0].HighBidder = "" },
		"duplicate cancel queue": func(gs *types.GenesisState) {
			gs.ClassCancelQueue = []types.QueueCursor{{ClassId: classID}, {ClassId: classID}}
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			gs := fresh()
			mut(gs)
			require.NotPanics(t, func() { err = gs.Validate() })
			require.Error(t, err)
		})
	}
}
