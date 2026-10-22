package keeper_test

import (
	"context"
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// Escrowed offers, English auctions and unit-listing updates
// (docs/x-artifact-spec.md §5.13, §7.27).

func TestTokenOffers(t *testing.T) {
	f := initFixture(t)
	classID := f.createClass(f.alice)
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	p := f.params()

	carolBefore := f.bank.bal(f.carol)
	resp, err := f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.carol.String(), ClassId: classID, TokenId: tok, Price: spark(4), Duration: 600})
	require.NoError(t, err)
	require.Equal(t, spark(4).Amount, carolBefore.Sub(f.bank.bal(f.carol)), "the price is escrowed")
	f.checkInvariants()

	_, err = f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.bob.String(), ClassId: classID, TokenId: tok, Price: spark(1), Duration: 600})
	require.ErrorIs(t, err, types.ErrInvalidRecipient, "no offers on your own token")
	_, err = f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.carol.String(), ClassId: classID, TokenId: tok, Price: sdkCoin(dream, 1), Duration: 600})
	require.ErrorIs(t, err, types.ErrDreamNotAccepted)

	// A listing is cancelled by accepting the offer.
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: classID, TokenId: tok, Price: spark(9), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.AcceptOffer(f.ctx, &types.MsgAcceptOffer{Seller: f.bob.String(), OfferId: resp.OfferId, ExpectedPrice: spark(3)})
	require.ErrorIs(t, err, types.ErrListingChanged)
	bobBefore := f.bank.bal(f.bob)
	acc, err := f.msg.AcceptOffer(f.ctx, &types.MsgAcceptOffer{Seller: f.bob.String(), OfferId: resp.OfferId, ExpectedPrice: spark(4)})
	require.NoError(t, err)
	require.Equal(t, f.carol.String(), f.token(classID, tok).Owner)
	require.Equal(t, keeperSettlement(4_000_000, p.SaleFeeBps, 500), f.bank.bal(f.bob).Sub(bobBefore))
	require.Equal(t, keeperSettlement(4_000_000, p.SaleFeeBps, 500).String(), acc.Proceeds)
	_, err = f.query.Listing(f.ctx, &types.QueryListingRequest{ClassId: classID, TokenId: tok})
	require.Error(t, err, "the listing went with the sale")
	f.checkInvariants()

	// Cancel refunds; expiry refunds through the EndBlocker.
	o2, err := f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.dave.String(), ClassId: classID, TokenId: tok, Price: spark(2), Duration: 60})
	require.NoError(t, err)
	_, err = f.msg.CancelOffer(f.ctx, &types.MsgCancelOffer{Bidder: f.carol.String(), OfferId: o2.OfferId})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	daveBefore := f.bank.bal(f.dave)
	f.advance(61, 1)
	f.endBlock()
	require.Equal(t, spark(2).Amount, f.bank.bal(f.dave).Sub(daveBefore))
	_, err = f.query.Offer(f.ctx, &types.QueryOfferRequest{OfferId: o2.OfferId})
	require.Error(t, err)

	// The per-bidder cap.
	f.setParams(func(p *types.Params) { p.MaxOffersPerBidder = 1 })
	_, err = f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.dave.String(), ClassId: classID, TokenId: tok, Price: spark(1), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.dave.String(), ClassId: classID, TokenId: tok, Price: spark(1), Duration: 600})
	require.ErrorIs(t, err, types.ErrTooManyOffers)
	f.checkInvariants()
}

func TestCollectionOffers(t *testing.T) {
	f := initFixture(t)
	classID := f.createClass(f.alice)
	toks := f.mint(f.alice, classID, f.bob, 2)
	o, err := f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.carol.String(), ClassId: classID, Price: spark(3), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.carol.String(), ClassId: classID, Price: spark(3), Duration: 600, ExpectedContentsHash: "x"})
	require.Error(t, err, "collection offers take no pins")

	// A collection offer never buys a token holding nested tokens.
	gear := f.createClass(f.alice)
	sword := f.mint(f.alice, gear, f.bob, 1)[0].TokenId
	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.bob.String(), Child: types.TokenRef{ClassId: gear, TokenId: sword},
		Parent: types.TokenRef{ClassId: classID, TokenId: toks[0].TokenId}})
	require.NoError(t, err)
	_, err = f.msg.AcceptOffer(f.ctx, &types.MsgAcceptOffer{Seller: f.bob.String(), OfferId: o.OfferId, TokenId: toks[0].TokenId, ExpectedPrice: spark(3)})
	require.ErrorIs(t, err, types.ErrContentsChanged)
	_, err = f.msg.AcceptOffer(f.ctx, &types.MsgAcceptOffer{Seller: f.bob.String(), OfferId: o.OfferId, TokenId: toks[1].TokenId, ExpectedPrice: spark(3)})
	require.NoError(t, err)
	require.Equal(t, f.carol.String(), f.token(classID, toks[1].TokenId).Owner)
	list, err := f.query.OffersByBidder(f.ctx, &types.QueryOffersByBidderRequest{Bidder: f.carol.String()})
	require.NoError(t, err)
	require.Empty(t, list.Offers)

	// Soulbound and Harberger classes take no offers.
	sb := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Flags.Transferable = false; m.RoyaltyBps = 0 })
	_, err = f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.carol.String(), ClassId: sb, Price: spark(1), Duration: 60})
	require.ErrorIs(t, err, types.ErrNotTransferable)
	f.checkInvariants()
}

func TestAuctions(t *testing.T) {
	f := initFixture(t)
	classID := f.createClass(f.alice)
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	p := f.params()

	start, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.bob.String(), ClassId: classID, TokenId: tok,
		ReservePrice: spark(2), MinIncrementBps: 1000, Duration: 1000})
	require.NoError(t, err)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_AUCTION, f.token(classID, tok).Lock)
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(),
		Entries: []types.TransferEntry{{ClassId: classID, TokenId: tok, Recipient: f.alice.String()}}})
	require.ErrorIs(t, err, types.ErrTokenLocked)

	bid := func(who sdk.AccAddress, amount int64) error {
		_, err := f.msg.Bid(f.ctx, &types.MsgBid{Bidder: who.String(), AuctionId: start.AuctionId, Amount: sdk.NewInt64Coin(bond, amount)})
		return err
	}
	require.ErrorIs(t, bid(f.carol, 1_999_999), types.ErrBidTooLow, "below the reserve")
	require.ErrorIs(t, bid(f.bob, 3_000_000), types.ErrInvalidRecipient, "no bidding on your own auction")
	require.NoError(t, bid(f.carol, 2_000_000))
	require.ErrorIs(t, bid(f.dave, 2_100_000), types.ErrBidTooLow, "must beat the high bid by 10%")
	carolBefore := f.bank.bal(f.carol)
	require.NoError(t, bid(f.dave, 2_200_000))
	require.Equal(t, math.NewInt(2_000_000), f.bank.bal(f.carol).Sub(carolBefore), "the outbid bidder is refunded at once")
	q, err := f.query.Auction(f.ctx, &types.QueryAuctionRequest{AuctionId: start.AuctionId})
	require.NoError(t, err)
	require.Equal(t, "2420000", q.MinNextBid)
	_, err = f.msg.CancelAuction(f.ctx, &types.MsgCancelAuction{Seller: f.bob.String(), AuctionId: start.AuctionId})
	require.ErrorIs(t, err, types.ErrAuctionActive, "an auction with bids runs to its end")
	f.checkInvariants()

	// A late bid extends the end (anti-sniping).
	f.advance(1000-p.AuctionExtension/2, 1)
	require.NoError(t, bid(f.carol, 2_500_000))
	q, err = f.query.Auction(f.ctx, &types.QueryAuctionRequest{AuctionId: start.AuctionId})
	require.NoError(t, err)
	require.Equal(t, f.ctx.BlockTime().Unix()+p.AuctionExtension, q.Auction.EndsAt)

	_, err = f.msg.SettleAuction(f.ctx, &types.MsgSettleAuction{Signer: f.alice.String(), AuctionId: start.AuctionId})
	require.ErrorIs(t, err, types.ErrAuctionActive)
	bobBefore := f.bank.bal(f.bob)
	f.advance(p.AuctionExtension+1, 1)
	f.endBlock()
	require.Equal(t, f.carol.String(), f.token(classID, tok).Owner, "the EndBlocker settled it")
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(classID, tok).Lock)
	require.Equal(t, keeperSettlement(2_500_000, p.SaleFeeBps, 500), f.bank.bal(f.bob).Sub(bobBefore))
	f.checkInvariants()

	// No bids: the token just unlocks; and a bidless auction can be cancelled.
	a2, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.carol.String(), ClassId: classID, TokenId: tok, ReservePrice: spark(1), Duration: 10})
	require.NoError(t, err)
	_, err = f.msg.CancelAuction(f.ctx, &types.MsgCancelAuction{Seller: f.carol.String(), AuctionId: a2.AuctionId})
	require.NoError(t, err)
	a3, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.carol.String(), ClassId: classID, TokenId: tok, ReservePrice: spark(1), Duration: 10})
	require.NoError(t, err)
	f.advance(11, 1)
	res, err := f.msg.SettleAuction(f.ctx, &types.MsgSettleAuction{Signer: f.dave.String(), AuctionId: a3.AuctionId})
	require.NoError(t, err)
	require.False(t, res.Sold)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(classID, tok).Lock)

	// A hide ends the auction and refunds the bid.
	a4, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.carol.String(), ClassId: classID, TokenId: tok, ReservePrice: spark(1), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.dave.String(), AuctionId: a4.AuctionId, Amount: spark(1)})
	require.NoError(t, err)
	daveBefore := f.bank.bal(f.dave)
	_, err = f.msg.HideContent(f.ctx, &types.MsgHideContent{Authority: f.ops.String(), TargetKind: types.HideTargetKind_HIDE_TARGET_KIND_TOKEN,
		ClassId: classID, TokenId: tok, Reason: 1})
	require.NoError(t, err)
	require.Equal(t, spark(1).Amount, f.bank.bal(f.dave).Sub(daveBefore))
	_, err = f.query.Auction(f.ctx, &types.QueryAuctionRequest{AuctionId: a4.AuctionId})
	require.Error(t, err)
	f.checkInvariants()
}

func TestMinNextBid(t *testing.T) {
	a := types.Auction{ReservePrice: sdk.NewInt64Coin(bond, 100), MinIncrementBps: 1000, HighBid: math.ZeroInt()}
	require.Equal(t, math.NewInt(100), keeper.MinNextBid(a))
	a.HighBidder, a.HighBid = "x", math.NewInt(105)
	require.Equal(t, math.NewInt(116), keeper.MinNextBid(a), "10% rounded up")
	a.MinIncrementBps = 0
	require.Equal(t, math.NewInt(106), keeper.MinNextBid(a), "always strictly higher")
}

func TestUpdateUnitListing(t *testing.T) {
	f := initFixture(t)
	classID := f.createEditionsClass(f.alice)
	ed := f.createEdition(f.alice, classID, 0, types.ImpactClaim{})
	f.mintUnits(f.alice, classID, ed, f.bob, 10)
	lr, err := f.msg.ListUnits(f.ctx, &types.MsgListUnits{Seller: f.bob.String(), ClassId: classID, EditionId: ed, Quantity: 4, UnitPrice: spark(1), Duration: 60})
	require.NoError(t, err)
	_, err = f.msg.UpdateUnitListing(f.ctx, &types.MsgUpdateUnitListing{Seller: f.carol.String(), ListingId: lr.ListingId, Quantity: 1, UnitPrice: spark(1), Duration: 60})
	require.ErrorIs(t, err, types.ErrNotTokenOwner)
	_, err = f.msg.UpdateUnitListing(f.ctx, &types.MsgUpdateUnitListing{Seller: f.bob.String(), ListingId: lr.ListingId, Quantity: 11, UnitPrice: spark(1), Duration: 60})
	require.ErrorIs(t, err, types.ErrInsufficientUnits)
	_, err = f.msg.UpdateUnitListing(f.ctx, &types.MsgUpdateUnitListing{Seller: f.bob.String(), ListingId: lr.ListingId, Quantity: 7, UnitPrice: spark(2), Duration: 3600})
	require.NoError(t, err)
	b := f.balanceOf(classID, ed, f.bob)
	require.Equal(t, uint64(7), b.Locked)
	require.Equal(t, uint64(3), b.Amount)
	_, err = f.msg.BuyUnits(f.ctx, &types.MsgBuyUnits{Buyer: f.carol.String(), ListingId: lr.ListingId, Quantity: 1, ExpectedUnitPrice: spark(1)})
	require.ErrorIs(t, err, types.ErrListingChanged, "the old price no longer buys")
	_, err = f.msg.UpdateUnitListing(f.ctx, &types.MsgUpdateUnitListing{Seller: f.bob.String(), ListingId: lr.ListingId, Quantity: 2, UnitPrice: spark(2), Duration: 3600})
	require.NoError(t, err)
	require.Equal(t, uint64(2), f.balanceOf(classID, ed, f.bob).Locked)
	// The old expiry is gone: the listing outlives its first 60 seconds.
	f.advance(61, 1)
	f.endBlock()
	_, err = f.query.UnitListing(f.ctx, &types.QueryUnitListingRequest{ListingId: lr.ListingId})
	require.NoError(t, err)
	f.checkInvariants()
}

// --- Renderer state v2 -------------------------------------------------------

type mockSeason struct {
	info    types.SeasonInfo
	members map[string]types.MemberSeason
}

func (m mockSeason) CurrentSeason(context.Context) (types.SeasonInfo, error) { return m.info, nil }
func (m mockSeason) MemberSeason(_ context.Context, addr string) (types.MemberSeason, bool, error) {
	s, ok := m.members[addr]
	return s, ok, nil
}

func TestRendererStateV2(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetSeasonKeeper(mockSeason{info: types.SeasonInfo{Number: 4, Status: "SEASON_STATUS_ACTIVE"},
		members: map[string]types.MemberSeason{f.bob.String(): {Level: 7, XP: 1234}}})
	pkgPath := "gno.land/p/demo/phoenix"
	pkg := types.GnoPackage{
		Files: []types.GnoSourceFile{{Name: "phoenix.gno", Body: "package phoenix\n"}},
		Pure:  true,
		Funcs: []types.GnoFuncSig{{Name: "RenderState", Params: []string{"uint64", "uint64", "string", "int64", "int64", "string"}, Results: []string{"string"}}},
	}
	f.gnovm.pkgs[pkgPath] = pkg
	resp, err := f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "Phoenix", AcceptedContentLicense: types.ContentLicense,
		Flags: types.ClassFlags{Transferable: true}, Renderer: types.Renderer{PkgPath: pkgPath, SourceHash: types.RendererSourceHash(pkg.Files), StateVersion: types.RenderStateV2}})
	require.NoError(t, err)
	hero := f.mint(f.alice, resp.ClassId, f.bob, 1)[0].TokenId
	gear := f.createClass(f.alice)
	sword := f.mint(f.alice, gear, f.bob, 1)[0].TokenId
	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.bob.String(), Child: types.TokenRef{ClassId: gear, TokenId: sword},
		Parent: types.TokenRef{ClassId: resp.ClassId, TokenId: hero}})
	require.NoError(t, err)

	var got string
	f.gnovm.eval = func(_, expr string) (string, error) {
		got = expr
		return `{"name":"phoenix"}`, nil
	}
	v := f.tokenQuery(resp.ClassId, hero)
	require.Equal(t, types.DisplayRenderer, v.DisplaySource)
	require.True(t, strings.HasPrefix(got, `RenderState(`), got)
	require.Contains(t, got, `\"v\":2`)
	require.Contains(t, got, `\"season_number\":4`)
	require.Contains(t, got, `\"owner_season_level\":7`)
	require.Contains(t, got, `\"children\":[\"`+u64s(gear)+`/`+u64s(sword)+`\"]`)
	require.Contains(t, got, `\"contents_hash\":\"`+v.ContentsHash+`\"`)
	require.NotEmpty(t, v.ContentsHash)
}
