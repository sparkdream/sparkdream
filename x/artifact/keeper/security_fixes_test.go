package keeper_test

import (
	"math"
	"testing"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
	reptypes "sparkdream/x/rep/types"
)

// ART-01: edition unit counters used to wrap. Minting 2^64-1 units and then
// 2 more wrapped supply to 1, so 1 uspark of retro funding paid each held
// unit about 1 uspark and drained every escrow in the module. The same
// sequence now fails at the first oversized mint and nothing moves.
func TestEditionSupplyOverflowDrainRefused(t *testing.T) {
	f := initFixture(t)
	// Unrelated escrow sits in the module: bob's offer.
	uniq := f.createClass(f.alice)
	toks := f.mint(f.alice, uniq, f.alice, 1)
	_, err := f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.bob.String(), ClassId: uniq, TokenId: toks[0].TokenId,
		Price: spark(500), Duration: 600})
	require.NoError(t, err)
	before := f.moduleBalance()

	cls := f.createEditionsClass(f.alice)
	ed := f.createEdition(f.alice, cls, 0, types.ImpactClaim{}) // uncapped
	mintUnits := func(to sdk.AccAddress, n uint64) error {
		_, err := f.msg.MintUnits(f.ctx, &types.MsgMintUnits{Minter: f.alice.String(), ClassId: cls, EditionId: ed,
			Entries: []types.UnitMintEntry{{Recipient: to.String(), Amount: n}}})
		return err
	}
	require.ErrorIs(t, mintUnits(f.alice, math.MaxUint64), types.ErrSupplyExceeded, "the drain's first step")
	require.ErrorIs(t, mintUnits(f.alice, types.HardMaxEditionSupply+1), types.ErrSupplyExceeded)

	// Up to the hard ceiling works; one unit more does not, even uncapped.
	require.NoError(t, mintUnits(f.alice, types.HardMaxEditionSupply-1))
	require.NoError(t, mintUnits(f.carol, 1))
	require.ErrorIs(t, mintUnits(f.carol, 2), types.ErrSupplyExceeded, "issued + n would wrap past the old check")
	require.Equal(t, types.HardMaxEditionSupply, f.edition(cls, ed).Supply)

	// Funding now pays out at most what was funded.
	_, err = f.msg.TransferUnits(f.ctx, &types.MsgTransferUnits{Sender: f.alice.String(),
		Entries: []types.UnitTransferEntry{{ClassId: cls, EditionId: ed, Recipient: f.dave.String(), Amount: before.Uint64()}}})
	require.NoError(t, err)
	_, err = f.msg.FundEdition(f.ctx, &types.MsgFundEdition{Funder: f.alice.String(), ClassId: cls, EditionId: ed,
		Amount: sdk.NewInt64Coin(bond, 1)})
	require.NoError(t, err)
	daveBefore := f.bank.bal(f.dave)
	_, _ = f.msg.ClaimEditionRewards(f.ctx, &types.MsgClaimEditionRewards{Holder: f.dave.String(),
		Refs: []types.EditionRef{{ClassId: cls, EditionId: ed}}})
	require.True(t, f.bank.bal(f.dave).Sub(daveBefore).LTE(sdkmath.NewInt(1)), "dave cannot claim the escrow")
	require.True(t, f.moduleBalance().GTE(before), "bob's escrow is intact")

	// The ceiling also bounds max_supply (at creation and when capping an
	// uncapped edition) and recipe quantities.
	_, err = f.msg.SetMaxSupply(f.ctx, &types.MsgSetMaxSupply{Owner: f.alice.String(), ClassId: cls, EditionId: ed,
		MaxSupply: types.HardMaxEditionSupply + 1})
	require.ErrorIs(t, err, types.ErrSupplyExceeded)
	_, err = f.msg.CreateEdition(f.ctx, &types.MsgCreateEdition{Minter: f.alice.String(), ClassId: cls, Metadata: meta("big"),
		MaxSupply: types.HardMaxEditionSupply + 1, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrSupplyExceeded)
	_, err = f.msg.AddCraftRecipe(f.ctx, &types.MsgAddCraftRecipe{Owner: f.alice.String(), ClassId: cls,
		Inputs: []types.CraftInput{{ClassId: uniq, Quantity: 1}}, OutputEditionId: ed, OutputQuantity: math.MaxUint64,
		AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrInvalidRecipe)
	f.checkInvariants()
}

// ART-02: a class handover keeps a locked royalty split.
func TestLockedRoyaltySurvivesHandover(t *testing.T) {
	f := initFixture(t)
	split := []types.RoyaltyShare{{Address: f.alice.String(), WeightBps: 7000}, {Address: f.carol.String(), WeightBps: 3000}}
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.RoyaltyBps = 500
		m.RoyaltyRecipients = split
	})
	_, err := f.msg.LockRoyalty(f.ctx, &types.MsgLockRoyalty{Owner: f.alice.String(), ClassId: id})
	require.NoError(t, err)
	_, err = f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: id, ProposedOwner: f.bob.String()})
	require.NoError(t, err)
	_, err = f.msg.AcceptClassOwner(f.ctx, &types.MsgAcceptClassOwner{ProposedOwner: f.bob.String(), ClassId: id})
	require.NoError(t, err)
	c := f.class(id)
	require.True(t, c.RoyaltyLocked)
	require.Equal(t, split, c.RoyaltyRecipients, "the collaborator keeps its share")
	require.Equal(t, f.bob.String(), c.PayoutAddress, "payout routing still moves")

	// An unlocked split is still reset to the new owner.
	open := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.RoyaltyBps = 500; m.RoyaltyRecipients = split })
	_, err = f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: open, ProposedOwner: f.bob.String()})
	require.NoError(t, err)
	_, err = f.msg.AcceptClassOwner(f.ctx, &types.MsgAcceptClassOwner{ProposedOwner: f.bob.String(), ClassId: open})
	require.NoError(t, err)
	require.Equal(t, types.SoleRoyaltyRecipient(f.bob.String()), f.class(open).RoyaltyRecipients)
}

// ART-03: settlement re-checks the winning bid's pins.
func TestAuctionPinsRecheckedAtSettlement(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	toks := f.mint(f.alice, id, f.alice, 2)
	start := func(tokenID uint64) uint64 {
		resp, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.alice.String(), ClassId: id, TokenId: tokenID,
			ReservePrice: spark(1), Duration: 600})
		require.NoError(t, err)
		return resp.AuctionId
	}
	swapped, plain := toks[0].TokenId, toks[1].TokenId
	a1, a2 := start(swapped), start(plain)
	_, err := f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.bob.String(), AuctionId: a1, Amount: spark(5),
		ExpectedMetadataHash: types.MetadataHash(f.token(id, swapped).Metadata)})
	require.NoError(t, err)
	_, err = f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.bob.String(), AuctionId: a2, Amount: spark(5)})
	require.NoError(t, err)
	// The seller swaps the media after the pinned bid.
	_, err = f.msg.UpdateToken(f.ctx, &types.MsgUpdateToken{Owner: f.alice.String(), ClassId: id, TokenId: swapped, Metadata: meta("junk")})
	require.NoError(t, err)
	_, err = f.msg.UpdateToken(f.ctx, &types.MsgUpdateToken{Owner: f.alice.String(), ClassId: id, TokenId: plain, Metadata: meta("new")})
	require.NoError(t, err)

	bobBefore := f.bank.bal(f.bob)
	f.advance(700, 1)
	f.endBlock()
	require.Equal(t, f.alice.String(), f.token(id, swapped).Owner, "the pinned bid is not filled with junk")
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(id, swapped).Lock)
	require.Equal(t, f.bob.String(), f.token(id, plain).Owner, "an unpinned bid accepts any metadata")
	require.Equal(t, bobBefore.Add(spark(5).Amount), f.bank.bal(f.bob), "the pinned bid came back")
	f.checkInvariants()
}

// ART-04: a token that could not be sold on its own cannot be nested into
// a sellable tree, and a tree holding one cannot be sold.
func TestNestRefusesUnsellableChild(t *testing.T) {
	f := initFixture(t)
	tickets := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{Transferable: true, Redeemable: true}
	})
	boxes := f.createClass(f.alice)
	ticket := f.mint(f.alice, tickets, f.bob, 1)[0].TokenId
	box := f.mint(f.alice, boxes, f.bob, 1)[0].TokenId
	_, err := f.msg.Redeem(f.ctx, &types.MsgRedeem{Signer: f.alice.String(), ClassId: tickets, TokenIds: []uint64{ticket}})
	require.NoError(t, err)
	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.bob.String(), Child: types.TokenRef{ClassId: tickets, TokenId: ticket},
		Parent: types.TokenRef{ClassId: boxes, TokenId: box}})
	require.ErrorIs(t, err, types.ErrNestNotAllowed, "a spent ticket cannot be laundered through a box")

	// A child that expires once nested blocks the sale of its tree.
	passes := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Expiry = types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_MARK, Term: 100, RenewalPrice: spark(1)}
	})
	pass := f.mint(f.alice, passes, f.bob, 1)[0].TokenId
	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.bob.String(), Child: types.TokenRef{ClassId: passes, TokenId: pass},
		Parent: types.TokenRef{ClassId: boxes, TokenId: box}})
	require.NoError(t, err)
	f.advance(200, 1)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: boxes, TokenId: box, Price: spark(1), Duration: 600})
	require.ErrorIs(t, err, types.ErrTokenExpired)
	_, err = f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.bob.String(), ClassId: boxes, TokenId: box,
		ReservePrice: spark(1), Duration: 600})
	require.ErrorIs(t, err, types.ErrTokenExpired)
}

// ART-05: MsgRedeem refuses tokens under auction or nested.
func TestRedeemRefusesAuctionAndNested(t *testing.T) {
	f := initFixture(t)
	tickets := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{Transferable: true, Redeemable: true}
	})
	boxes := f.createClass(f.alice)
	r := f.mint(f.alice, tickets, f.bob, 2)
	box := f.mint(f.alice, boxes, f.bob, 1)[0].TokenId
	resp, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.bob.String(), ClassId: tickets, TokenId: r[0].TokenId,
		ReservePrice: spark(1), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.carol.String(), AuctionId: resp.AuctionId, Amount: spark(2)})
	require.NoError(t, err)
	_, err = f.msg.Redeem(f.ctx, &types.MsgRedeem{Signer: f.alice.String(), ClassId: tickets, TokenIds: []uint64{r[0].TokenId}})
	require.ErrorIs(t, err, types.ErrTokenLocked, "the issuer cannot kill a running auction")

	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.bob.String(), Child: types.TokenRef{ClassId: tickets, TokenId: r[1].TokenId},
		Parent: types.TokenRef{ClassId: boxes, TokenId: box}})
	require.NoError(t, err)
	_, err = f.msg.Redeem(f.ctx, &types.MsgRedeem{Signer: f.alice.String(), ClassId: tickets, TokenIds: []uint64{r[1].TokenId}})
	require.ErrorIs(t, err, types.ErrTokenLocked)
}

// ART-06: a settlement that errors ends the auction unsold instead of
// stranding the high bid; a failed Harberger pass re-queues the token.
func TestAuctionSettlementFailureRefunds(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	tok := f.mint(f.alice, id, f.alice, 1)[0].TokenId
	resp, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.alice.String(), ClassId: id, TokenId: tok,
		ReservePrice: spark(1), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.bob.String(), AuctionId: resp.AuctionId, Amount: spark(5)})
	require.NoError(t, err)
	bobBefore := f.bank.bal(f.bob)
	f.bank.failTo["community_pool"] = true // the sale fee cannot be paid
	f.advance(700, 1)
	f.endBlock()
	_, err = f.keeper.Auctions.Get(f.ctx, resp.AuctionId)
	require.ErrorIs(t, err, collections.ErrNotFound)
	require.Equal(t, bobBefore.Add(spark(5).Amount), f.bank.bal(f.bob), "the high bid came back")
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(id, tok).Lock)
	require.Equal(t, f.alice.String(), f.token(id, tok).Owner)
	delete(f.bank.failTo, "community_pool")

	// MsgSettleAuction falls back the same way.
	resp, err = f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.alice.String(), ClassId: id, TokenId: tok,
		ReservePrice: spark(1), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.bob.String(), AuctionId: resp.AuctionId, Amount: spark(5)})
	require.NoError(t, err)
	f.bank.failTo["community_pool"] = true
	f.advance(700, 1)
	settled, err := f.msg.SettleAuction(f.ctx, &types.MsgSettleAuction{Signer: f.carol.String(), AuctionId: resp.AuctionId})
	require.NoError(t, err)
	require.False(t, settled.Sold)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(id, tok).Lock)
	delete(f.bank.failTo, "community_pool")
	f.checkInvariants()
}

func TestHarbergerPassFailureRequeues(t *testing.T) {
	f := initFixture(t)
	sess := f.withSession()
	policy := types.HarbergerPolicy{TaxBpsPerYear: 1000, TaxPeriod: 86400, ForeclosureDuration: 864000}
	classID := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Harberger = policy })
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	price := sdkmath.NewInt(365_000_000)
	start := f.ctx.BlockTime().Unix()
	sess.grants[1] = &types.RecurringPullInfo{Granter: f.bob.String(), Grantee: moduleAddress(), Active: true,
		Amount: sdk.NewCoin(bond, policy.TaxPerPeriod(price)), PeriodSeconds: 86400, NextClaim: start + 86400, ExpiresAt: start + 10*86400}
	_, err := f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.bob.String(), ClassId: classID, TokenId: tok, Price: price, GrantId: 1})
	require.NoError(t, err)
	due := f.token(classID, tok).Harberger.NextDue

	sess.panics[1] = true // an internal failure, not a failed pull
	f.advance(86400, 1)
	f.endBlock()
	h := f.token(classID, tok).Harberger
	require.Zero(t, h.ForeclosedAt, "an internal failure does not foreclose")
	require.Equal(t, due+86400, h.NextDue, "re-queued one period later")
	has, err := f.keeper.HarbergerDue.Has(f.ctx, collections.Join3(h.NextDue, classID, tok))
	require.NoError(t, err)
	require.True(t, has, "tax keeps being collected")
	f.checkInvariants()

	// Once the bug is gone the next pass collects again.
	sess.panics[1] = false
	f.advance(86400, 1)
	f.endBlock()
	require.Equal(t, []uint64{1}, sess.claims)
}

// ART-07: DeleteClass removes the class's own craft recipes.
func TestDeleteClassClearsRecipes(t *testing.T) {
	f := initFixture(t)
	shards := f.createClass(f.alice)
	relics := f.createClass(f.alice)
	_, err := f.msg.AddCraftRecipe(f.ctx, &types.MsgAddCraftRecipe{Owner: f.alice.String(), ClassId: relics,
		Inputs: []types.CraftInput{{ClassId: shards, Quantity: 1}}, OutputMetadata: meta("relic"), AcceptedContentLicense: types.ContentLicense})
	require.NoError(t, err)
	_, err = f.msg.DeleteClass(f.ctx, &types.MsgDeleteClass{Owner: f.alice.String(), ClassId: relics})
	require.NoError(t, err)
	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Empty(t, gs.CraftRecipes)
	require.NoError(t, gs.Validate())
}

// ART-08: a walk error is never overwritten by a later sequence peek.
func TestExportGenesisReportsWalkError(t *testing.T) {
	f := initFixture(t)
	// An undecodable edition record makes the Editions walk fail; the
	// OfferSeq/AuctionSeq peeks after it used to reset the error to nil.
	key := append(append([]byte{}, types.EditionsKey...), make([]byte, 16)...)
	f.ctx.KVStore(f.key).Set(key, []byte{0xff, 0xff, 0xff})
	_, err := f.keeper.ExportGenesis(f.ctx)
	require.Error(t, err)
}

// ART-10: MsgRevokeUnits resolves a migrated holder through the alias.
func TestRevokeUnitsMigratedHolder(t *testing.T) {
	f := initFixture(t)
	cls := f.createEditionsClass(f.alice, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}
		m.RoyaltyBps = 0
	})
	ed := f.createEdition(f.alice, cls, 0, types.ImpactClaim{})
	f.mintUnits(f.alice, cls, ed, f.alice, 5)
	_, err := f.msg.TransferUnits(f.ctx, &types.MsgTransferUnits{Sender: f.alice.String(),
		Entries: []types.UnitTransferEntry{{ClassId: cls, EditionId: ed, Recipient: f.bob.String(), Amount: 1}}})
	require.ErrorIs(t, err, types.ErrNotTransferable)
	_, err = f.msg.MintUnits(f.ctx, &types.MsgMintUnits{Minter: f.alice.String(), ClassId: cls, EditionId: ed,
		Entries: []types.UnitMintEntry{{Recipient: f.bob.String(), Amount: 3}}})
	require.NoError(t, err)
	if f.balanceOf(cls, ed, f.bob).Amount == 0 { // parked in bob's inbox: accept it
		gs, _ := f.keeper.ExportGenesis(f.ctx)
		require.Len(t, gs.PendingUnits, 1)
		_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(),
			Entries: []types.AcceptEntry{{ClassId: 0, TokenId: gs.PendingUnits[0].Id}}})
		require.NoError(t, err)
	}
	require.Equal(t, uint64(3), f.balanceOf(cls, ed, f.bob).Amount)

	f.migrate(f.bob.String(), f.dave.String())
	_, err = f.msg.RevokeUnits(f.ctx, &types.MsgRevokeUnits{Owner: f.alice.String(), ClassId: cls, EditionId: ed,
		Holder: f.dave.String(), Amount: 2, Reason: "misissued"})
	require.NoError(t, err, "the successor's address finds the predecessor's balance")
	require.Equal(t, uint64(1), f.balanceOf(cls, ed, f.bob).Amount)
	f.checkInvariants()
}

// ART-11: the single Offer and UnitListing queries filter expired records.
func TestExpiredOfferQueriesFiltered(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	tok := f.mint(f.alice, id, f.alice, 1)[0].TokenId
	resp, err := f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.bob.String(), ClassId: id, TokenId: tok, Price: spark(1), Duration: 60})
	require.NoError(t, err)
	_, err = f.query.Offer(f.ctx, &types.QueryOfferRequest{OfferId: resp.OfferId})
	require.NoError(t, err)
	f.advance(120, 0) // past expiry, EndBlocker not run
	_, err = f.query.Offer(f.ctx, &types.QueryOfferRequest{OfferId: resp.OfferId})
	require.Error(t, err)
	byBidder, err := f.query.OffersByBidder(f.ctx, &types.QueryOffersByBidderRequest{Bidder: f.bob.String()})
	require.NoError(t, err)
	require.Empty(t, byBidder.Offers)
}

// ART-13: an opened FIXED price is locked like any curve.
func TestFixedMintPriceLockedOnceOpen(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(10)}
	})
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 1, MaxPrice: spark(10)})
	require.NoError(t, err)
	mp := f.class(id).MintPolicy
	mp.Price = sdk.NewInt64Coin(bond, 100_000)
	_, err = f.msg.SetMintPolicy(f.ctx, &types.MsgSetMintPolicy{Owner: f.alice.String(), ClassId: id, MintPolicy: mp})
	require.ErrorIs(t, err, types.ErrMintPolicyLocked, "no price cut under the early buyers")
	mp = f.class(id).MintPolicy
	mp.Pricing, mp.FloorPrice, mp.StartTime, mp.EndTime = types.MintPricing_MINT_PRICING_DUTCH, spark(1).Amount, f.ctx.BlockTime().Unix(), f.ctx.BlockTime().Unix()+100
	_, err = f.msg.SetMintPolicy(f.ctx, &types.MsgSetMintPolicy{Owner: f.alice.String(), ClassId: id, MintPolicy: mp})
	require.ErrorIs(t, err, types.ErrMintPolicyLocked, "no switch to a curve either")
	mp = f.class(id).MintPolicy
	mp.PublicMintEnabled, mp.PerAddressLimit = false, 2
	_, err = f.msg.SetMintPolicy(f.ctx, &types.MsgSetMintPolicy{Owner: f.alice.String(), ClassId: id, MintPolicy: mp})
	require.NoError(t, err, "disable and per-address cap still move")
}

// ART-14: a handover proposed to a member's old address no longer vetoes
// the migration; the successor accepts it and the tombstone cannot.
func TestHandoverFollowsMigration(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	_, err := f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: id, ProposedOwner: f.bob.String()})
	require.NoError(t, err)
	hooks := keeper.NewMigrationHooks(&f.keeper)
	require.NoError(t, hooks.BeforeMemberMigration(f.ctx, f.bob, f.dave, reptypes.MigrationPath_MIGRATION_PATH_RECOVERY))
	f.migrate(f.bob.String(), f.dave.String())

	_, err = f.msg.AcceptClassOwner(f.ctx, &types.MsgAcceptClassOwner{ProposedOwner: f.bob.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrPendingOwnerNotFound, "the tombstone cannot accept")
	_, err = f.msg.AcceptClassOwner(f.ctx, &types.MsgAcceptClassOwner{ProposedOwner: f.dave.String(), ClassId: id})
	require.NoError(t, err)
	require.Equal(t, f.dave.String(), f.class(id).Owner)
}

// ART-15: minting units to one's own earlier address is self-delivery.
func TestMintUnitsToOwnPredecessorDelivers(t *testing.T) {
	f := initFixture(t)
	cls := f.createEditionsClass(f.alice, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}
		m.RoyaltyBps = 0
	})
	ed := f.createEdition(f.alice, cls, 0, types.ImpactClaim{})
	old := f.alice.String()
	f.migrate(old, f.dave.String())
	_, err := f.msg.MintUnits(f.ctx, &types.MsgMintUnits{Minter: f.dave.String(), ClassId: cls, EditionId: ed,
		Entries: []types.UnitMintEntry{{Recipient: old, Amount: 4}}})
	require.NoError(t, err)
	require.Equal(t, uint64(4), f.balanceOf(cls, ed, f.alice).Amount, "delivered directly, not parked in an inbox")
}
