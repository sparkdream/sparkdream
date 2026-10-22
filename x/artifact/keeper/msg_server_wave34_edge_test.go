package keeper_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

// Edge paths of waves 3 and 4: refunds on rejected and expired unit
// items, unit-listing expiry, revocation and expiry inside trees and
// rentals, Harberger gifts and exclusions, and state-reading renderers.

func TestPendingUnitsRejectAndExpire(t *testing.T) {
	f := initFixture(t)
	classID := f.createEditionsClass(f.alice)
	ed := f.createEdition(f.alice, classID, 0, types.ImpactClaim{})
	f.mintUnits(f.alice, classID, ed, f.dave, 6)
	p := f.params()

	send := func(amount uint64) uint64 {
		resp, err := f.msg.TransferUnits(f.ctx, &types.MsgTransferUnits{Sender: f.dave.String(),
			Entries: []types.UnitTransferEntry{{ClassId: classID, EditionId: ed, Recipient: f.bob.String(), Amount: amount}}})
		require.NoError(t, err)
		require.False(t, resp.Results[0].Delivered)
		return resp.Results[0].PendingId
	}

	// Rejected: units unlock and the escrowed deposit comes back; the
	// inbox fee stays burned.
	daveBefore := f.bank.bal(f.dave)
	pid := send(2)
	_, err := f.msg.RejectIncoming(f.ctx, &types.MsgRejectIncoming{Recipient: f.bob.String(), Refs: []types.TokenRef{{ClassId: 0, TokenId: pid}}})
	require.NoError(t, err)
	require.Equal(t, p.InboxFee, daveBefore.Sub(f.bank.bal(f.dave)))
	b := f.balanceOf(classID, ed, f.dave)
	require.Equal(t, uint64(6), b.Amount)
	require.Zero(t, b.Locked)

	// Only the recipient may accept, and the sender may cancel.
	pid = send(1)
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.carol.String(), Entries: []types.AcceptEntry{{ClassId: 0, TokenId: pid}}})
	require.ErrorIs(t, err, types.ErrPendingNotFound)
	_, err = f.msg.CancelOutgoing(f.ctx, &types.MsgCancelOutgoing{Signer: f.carol.String(), Refs: []types.TokenRef{{ClassId: 0, TokenId: pid}}})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = f.msg.CancelOutgoing(f.ctx, &types.MsgCancelOutgoing{Signer: f.dave.String(), Refs: []types.TokenRef{{ClassId: 0, TokenId: pid}}})
	require.NoError(t, err)

	// Expired: the EndBlocker resolves it like a rejection.
	pid = send(3)
	f.advance(p.PendingTtl+1, 1)
	f.endBlock()
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(), Entries: []types.AcceptEntry{{ClassId: 0, TokenId: pid}}})
	require.ErrorIs(t, err, types.ErrPendingNotFound)
	require.Zero(t, f.balanceOf(classID, ed, f.dave).Locked)

	// A pending unit mint to an inbox-only recipient reserves units.
	_, err = f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.carol.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	res := f.mintUnits(f.alice, classID, ed, f.carol, 4)
	require.False(t, res.Delivered)
	require.Equal(t, uint64(4), f.edition(classID, ed).Reserved)
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.carol.String(), Entries: []types.AcceptEntry{{ClassId: 0, TokenId: res.PendingId}}})
	require.NoError(t, err)
	e := f.edition(classID, ed)
	require.Zero(t, e.Reserved)
	require.Equal(t, uint64(10), e.Supply)
	f.checkInvariants()
}

func TestUnitListingExpiry(t *testing.T) {
	f := initFixture(t)
	classID := f.createEditionsClass(f.alice)
	ed := f.createEdition(f.alice, classID, 0, types.ImpactClaim{})
	f.mintUnits(f.alice, classID, ed, f.bob, 5)
	lr, err := f.msg.ListUnits(f.ctx, &types.MsgListUnits{Seller: f.bob.String(), ClassId: classID, EditionId: ed, Quantity: 5, UnitPrice: spark(1), Duration: 60})
	require.NoError(t, err)
	_, err = f.msg.TransferUnits(f.ctx, &types.MsgTransferUnits{Sender: f.bob.String(),
		Entries: []types.UnitTransferEntry{{ClassId: classID, EditionId: ed, Recipient: f.carol.String(), Amount: 1}}})
	require.ErrorIs(t, err, types.ErrInsufficientUnits, "listed units are locked")
	f.advance(61, 1)
	_, err = f.msg.BuyUnits(f.ctx, &types.MsgBuyUnits{Buyer: f.carol.String(), ListingId: lr.ListingId, Quantity: 1, ExpectedUnitPrice: spark(1)})
	require.ErrorIs(t, err, types.ErrListingExpired)
	f.endBlock()
	_, err = f.query.UnitListing(f.ctx, &types.QueryUnitListingRequest{ListingId: lr.ListingId})
	require.Error(t, err)
	require.Equal(t, uint64(5), f.balanceOf(classID, ed, f.bob).Amount)
}

func TestRevokeNestedSoulboundChild(t *testing.T) {
	f := initFixture(t)
	soulbound := func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}
		m.RoyaltyBps = 0
	}
	wall := f.createClass(f.alice, soulbound)
	badges := f.createClass(f.alice, soulbound)
	frame := f.mint(f.alice, wall, f.alice, 1)[0].TokenId
	badge := f.mint(f.alice, badges, f.alice, 1)[0].TokenId
	_, err := f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.alice.String(), Child: types.TokenRef{ClassId: badges, TokenId: badge},
		Parent: types.TokenRef{ClassId: wall, TokenId: frame}})
	require.NoError(t, err)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.alice.String(), Refs: []types.TokenRef{{ClassId: badges, TokenId: badge}}})
	require.ErrorIs(t, err, types.ErrTokenLocked, "the holder detaches first")
	_, err = f.msg.Revoke(f.ctx, &types.MsgRevoke{Owner: f.alice.String(), ClassId: badges, TokenIds: []uint64{badge}, Reason: "expired membership"})
	require.NoError(t, err, "revocation detaches a nested token itself")
	require.Zero(t, f.token(wall, frame).Descendants)
	f.checkInvariants()
}

func TestExpiryBurnsRentedToken(t *testing.T) {
	f := initFixture(t)
	classID := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Expiry = types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_BURN, Term: 1000}
	})
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	offer, err := f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.bob.String(), ClassId: classID, TokenId: tok,
		Price: spark(0), Duration: 2000, OfferDuration: 60})
	require.NoError(t, err)
	_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: classID, TokenId: tok,
		ExpectedPrice: spark(0), ExpectedNonce: offer.Nonce})
	require.ErrorIs(t, err, types.ErrNotRentable, "a rental never outlasts its token")
	offer, err = f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.bob.String(), ClassId: classID, TokenId: tok,
		Price: spark(0), Duration: 900, OfferDuration: 60})
	require.NoError(t, err)
	_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: classID, TokenId: tok,
		ExpectedPrice: spark(0), ExpectedNonce: offer.Nonce})
	require.NoError(t, err)
	f.advance(1001, 1)
	f.endBlock()
	_, found, err := f.keeper.GetToken(f.ctx, classID, tok)
	require.NoError(t, err)
	require.False(t, found, "the rental ended, then the token expired")
}

func TestHarbergerGiftsAndExclusions(t *testing.T) {
	f := initFixture(t)
	policy := types.HarbergerPolicy{TaxBpsPerYear: 1000, TaxPeriod: 100, ForeclosureDuration: 1000}
	classID := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Harberger = policy
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(10)}
	})
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: classID, Quantity: 1, MaxPrice: spark(10)})
	require.NoError(t, err)
	require.Equal(t, spark(10).Amount, f.token(classID, 1).Harberger.Price, "assessed at the mint price")

	// Grace passes without a grant: the price falls. A gift carries the
	// current price, so moving the token never sheds a foreclosure.
	f.advance(600, 1)
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(),
		Entries: []types.TransferEntry{{ClassId: classID, TokenId: 1, Recipient: f.carol.String()}}})
	require.NoError(t, err)
	h := f.token(classID, 1).Harberger
	require.Equal(t, math.NewInt(5_000_000), h.Price)
	require.Zero(t, h.ForeclosedAt)

	// No rentals or nesting.
	_, err = f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.carol.String(), ClassId: classID, TokenId: 1,
		Price: spark(1), Duration: 60, OfferDuration: 60})
	require.ErrorIs(t, err, types.ErrNotRentable)
	other := f.createClass(f.alice)
	box := f.mint(f.alice, other, f.carol, 1)[0].TokenId
	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.carol.String(), Child: types.TokenRef{ClassId: classID, TokenId: 1},
		Parent: types.TokenRef{ClassId: other, TokenId: box}})
	require.ErrorIs(t, err, types.ErrNestNotAllowed)

	// Hidden tokens are never force-sold; the circuit breaker stops it too.
	_, err = f.msg.HideContent(f.ctx, &types.MsgHideContent{Authority: f.ops.String(), TargetKind: types.HideTargetKind_HIDE_TARGET_KIND_TOKEN,
		ClassId: classID, TokenId: 1, Reason: 1})
	require.NoError(t, err)
	_, err = f.msg.ForceBuy(f.ctx, &types.MsgForceBuy{Buyer: f.dave.String(), ClassId: classID, TokenId: 1, MaxPrice: spark(10)})
	require.ErrorIs(t, err, types.ErrContentHidden)

	// Without x/session wired, no grant can be named.
	_, err = f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.carol.String(), ClassId: classID, TokenId: 1,
		Price: spark(3).Amount, GrantId: 7})
	require.ErrorIs(t, err, types.ErrInvalidHarbergerGrant)
	f.checkInvariants()
}

func TestRendererStateBinding(t *testing.T) {
	f := initFixture(t)
	pkgPath := "gno.land/p/demo/zenith"
	pkg := types.GnoPackage{
		Files: []types.GnoSourceFile{{Name: "zenith.gno", Body: "package zenith\n"}},
		Pure:  true,
		Funcs: []types.GnoFuncSig{{Name: "RenderState", Params: []string{"uint64", "uint64", "string", "int64", "int64", "string"}, Results: []string{"string"}}},
	}
	f.gnovm.pkgs[pkgPath] = pkg
	hash := types.RendererSourceHash(pkg.Files)
	_, err := f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "Zenith", AcceptedContentLicense: types.ContentLicense,
		Flags: types.ClassFlags{Transferable: true}, Renderer: types.Renderer{PkgPath: pkgPath, SourceHash: hash}})
	require.ErrorIs(t, err, types.ErrInvalidRenderer, "state_version 0 needs Render")
	resp, err := f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "Zenith", AcceptedContentLicense: types.ContentLicense,
		Flags: types.ClassFlags{Transferable: true}, Renderer: types.Renderer{PkgPath: pkgPath, SourceHash: hash, StateVersion: types.RenderStateV1}})
	require.NoError(t, err)
	f.mint(f.alice, resp.ClassId, f.bob, 1)

	var got string
	f.gnovm.eval = func(_, expr string) (string, error) {
		got = expr
		return `{"name":"zenith"}`, nil
	}
	v := f.tokenQuery(resp.ClassId, 1)
	require.Equal(t, types.DisplayRenderer, v.DisplaySource)
	require.True(t, strings.HasPrefix(got, "RenderState("), got)
	require.Contains(t, got, `\"owner\":\"`+f.bob.String()+`\"`)
	require.Contains(t, got, `\"owner_member\":true`)
}
