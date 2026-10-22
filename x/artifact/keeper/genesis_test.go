package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/artifact/types"
)

// populate builds a state touching every exported collection.
func populate(f *fixture) uint64 {
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Minters = []string{f.carol.String()} })
	f.mint(f.alice, id, f.alice, 4)
	_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(f.t, err)
	f.mint(f.alice, id, f.dave, 1) // pending mint
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.alice.String(), Entries: []types.TransferEntry{{ClassId: id, TokenId: 1, Recipient: f.dave.String()}}})
	require.NoError(f.t, err) // pending transfer
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: 2, Price: spark(3), Duration: 600})
	require.NoError(f.t, err)
	_, err = f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: id, ProposedOwner: f.bob.String()})
	require.NoError(f.t, err)
	_, err = f.msg.HideContent(f.ctx, &types.MsgHideContent{Authority: f.sentinel.String(), TargetKind: types.HideTargetKind_HIDE_TARGET_KIND_TOKEN,
		ClassId: id, TokenId: 3, Reason: commontypes.ModerationReason_MODERATION_REASON_SPAM})
	require.NoError(f.t, err)
	id2 := f.createClass(f.bob, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(1)}
	})
	_, err = f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.carol.String(), ClassId: id2, Quantity: 2, MaxPrice: spark(1)})
	require.NoError(f.t, err)
	f.checkInvariants()
	return id
}

func TestGenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	populate(f)
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.Classes, 2)
	require.Len(t, exported.PendingMints, 1)
	require.Len(t, exported.PendingTransfers, 1)
	require.Len(t, exported.Listings, 1)
	require.Len(t, exported.PendingClassOwners, 1)
	require.Len(t, exported.HideRecords, 1)
	require.Len(t, exported.ReceivePolicies, 1)
	require.Len(t, exported.PublicMintCounts, 1)

	g := initFixture(t)
	// The fresh fixture's module account must hold the escrow too.
	g.bank.balances[moduleAddr(types.ModuleName)] = f.bank.balances[moduleAddr(types.ModuleName)]
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	g.checkInvariants()
	reexported, err := g.keeper.ExportGenesis(g.ctx)
	require.NoError(t, err)
	require.Equal(t, exported, reexported)

	// Derived counters were rebuilt: the next class id and inbox caps work.
	require.Equal(t, uint64(3), g.createClass(g.alice))
	inbox, err := g.query.Inbox(g.ctx, &types.QueryInboxRequest{Address: g.dave.String()})
	require.NoError(t, err)
	require.Len(t, inbox.Items, 2)
	require.Equal(t, uint32(2), inbox.InboxCount)
}

func TestGenesisValidate(t *testing.T) {
	f := initFixture(t)
	populate(f)
	good, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)

	cases := []struct {
		name string
		mut  func(gs *types.GenesisState)
	}{
		{"class above seq", func(gs *types.GenesisState) { gs.ClassSeq = 1 }},
		{"supply mismatch", func(gs *types.GenesisState) { gs.Classes[0].Supply++ }},
		{"reserved mismatch", func(gs *types.GenesisState) { gs.Classes[0].ReservedSupply = 0 }},
		{"token id beyond next", func(gs *types.GenesisState) { gs.Classes[0].NextTokenId = 2 }},
		{"listing without lock", func(gs *types.GenesisState) {
			for i := range gs.Tokens {
				gs.Tokens[i].Lock = types.TokenLock_TOKEN_LOCK_NONE
			}
		}},
		{"issuer-burnable transferable", func(gs *types.GenesisState) {
			gs.Classes[0].Flags.BurnAuthorization = types.BurnAuthorization_BURN_AUTHORIZATION_ISSUER
		}},
		{"unknown burn authorization", func(gs *types.GenesisState) { gs.Classes[0].Flags.BurnAuthorization = 9 }},
		{"empty royalty split", func(gs *types.GenesisState) { gs.Classes[0].RoyaltyRecipients = nil }},
		{"hide above seq", func(gs *types.GenesisState) { gs.HideSeq = 0 }},
		{"unspecified policy", func(gs *types.GenesisState) { gs.ReceivePolicies[0].Policy = 0 }},
		{"bad params", func(gs *types.GenesisState) { gs.Params.SaleFeeBps = 5000 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs, err := f.keeper.ExportGenesis(f.ctx)
			require.NoError(t, err)
			tc.mut(gs)
			require.Error(t, gs.Validate())
		})
	}
	require.NoError(t, good.Validate())
	require.NoError(t, types.DefaultGenesis().Validate())
}

func TestUpdateParamsAndOperationalParams(t *testing.T) {
	f := initFixture(t)
	p := f.params()
	p.MaxBatchSize = 20
	_, err := f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.alice.String(), Params: p})
	require.ErrorIs(t, err, types.ErrInvalidSigner)
	_, err = f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.gov, Params: p})
	require.NoError(t, err)
	require.Equal(t, uint32(20), f.params().MaxBatchSize)

	bad := p
	bad.AllowedUriSchemes = []string{"https", "http"}
	_, err = f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.gov, Params: bad})
	require.ErrorIs(t, err, types.ErrInvalidParams)

	op := f.params().OperationalParams()
	op.InboxFee = op.InboxFee.MulRaw(2)
	_, err = f.msg.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.dave.String(), OperationalParams: op})
	require.ErrorIs(t, err, types.ErrInvalidSigner)
	_, err = f.msg.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.ops.String(), OperationalParams: op})
	require.NoError(t, err)
	require.Equal(t, op.InboxFee, f.params().InboxFee)
	require.Equal(t, uint32(20), f.params().MaxBatchSize, "governance fields untouched")

	// Hard bounds stop an individual ops member from zeroing spam backing.
	zero := op
	zero.TokenDeposit = zero.TokenDeposit.SubRaw(zero.TokenDeposit.Int64())
	_, err = f.msg.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.ops.String(), OperationalParams: zero})
	require.ErrorIs(t, err, types.ErrInvalidParams)
}

// populateWave34 builds editions (with a pending unit transfer, a unit
// listing and retro funding), a crafting recipe, a rental, a rental offer,
// a nested tree and a Harberger token with a tax grant.
func populateWave34(f *fixture) {
	sess := f.withSession()
	ed := f.createEditionsClass(f.alice)
	cert := f.createEdition(f.alice, ed, 100, types.ImpactClaim{Scope: "aurora survey", Contributors: []string{f.alice.String()}})
	f.mintUnits(f.alice, ed, cert, f.bob, 10)
	f.mintUnits(f.alice, ed, cert, f.dave, 4)
	_, err := f.msg.TransferUnits(f.ctx, &types.MsgTransferUnits{Sender: f.dave.String(),
		Entries: []types.UnitTransferEntry{{ClassId: ed, EditionId: cert, Recipient: f.carol.String(), Amount: 1}}})
	require.NoError(f.t, err) // pending: dave is untrusted
	_, err = f.msg.ListUnits(f.ctx, &types.MsgListUnits{Seller: f.bob.String(), ClassId: ed, EditionId: cert, Quantity: 3, UnitPrice: spark(1), Duration: 600})
	require.NoError(f.t, err)
	_, err = f.msg.FundEdition(f.ctx, &types.MsgFundEdition{Funder: f.carol.String(), ClassId: ed, EditionId: cert, Amount: spark(7)})
	require.NoError(f.t, err)

	gear := f.createClass(f.alice)
	g := f.mint(f.alice, gear, f.bob, 3)
	_, err = f.msg.AddCraftRecipe(f.ctx, &types.MsgAddCraftRecipe{Owner: f.alice.String(), ClassId: gear,
		Inputs: []types.CraftInput{{ClassId: gear, Quantity: 2}}, OutputMetadata: meta("forged"), AcceptedContentLicense: types.ContentLicense})
	require.NoError(f.t, err)
	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.bob.String(), Child: types.TokenRef{ClassId: gear, TokenId: g[1].TokenId},
		Parent: types.TokenRef{ClassId: gear, TokenId: g[0].TokenId}})
	require.NoError(f.t, err)
	offer, err := f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.bob.String(), ClassId: gear, TokenId: g[0].TokenId,
		Price: spark(1), Duration: 600, OfferDuration: 600})
	require.NoError(f.t, err)
	hash, err := f.keeper.ContentsHash(f.ctx, f.token(gear, g[0].TokenId))
	require.NoError(f.t, err)
	_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: gear, TokenId: g[0].TokenId,
		ExpectedPrice: spark(1), ExpectedNonce: offer.Nonce, ExpectedContentsHash: hash})
	require.NoError(f.t, err)
	_, err = f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.bob.String(), ClassId: gear, TokenId: g[2].TokenId,
		Price: spark(1), Duration: 600, OfferDuration: 600})
	require.NoError(f.t, err)

	policy := types.HarbergerPolicy{TaxBpsPerYear: 1000, TaxPeriod: 86400, ForeclosureDuration: 86400}
	plots := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Harberger = policy })
	plot := f.mint(f.alice, plots, f.bob, 1)[0].TokenId
	now := f.ctx.BlockTime().Unix()
	sess.grants[1] = &types.RecurringPullInfo{Granter: f.bob.String(), Grantee: moduleAddress(), Active: true,
		Amount: sdkCoin(bond, 1_000_000), PeriodSeconds: 86400, NextClaim: now + 86400, ExpiresAt: now + 30*86400}
	_, err = f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.bob.String(), ClassId: plots, TokenId: plot,
		Price: spark(10).Amount, GrantId: 1})
	require.NoError(f.t, err)
	f.checkInvariants()
}

func TestGenesisRoundTripWave34(t *testing.T) {
	f := initFixture(t)
	populateWave34(f)
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.Editions, 1)
	require.Len(t, exported.Balances, 2) // bob and dave; carol's units are pending
	require.Len(t, exported.PendingUnits, 1)
	require.Len(t, exported.UnitListings, 1)
	require.Len(t, exported.CraftRecipes, 1)
	require.Len(t, exported.Rentals, 1)
	require.Len(t, exported.RentalOffers, 1)

	g := initFixture(t)
	g.withSession()
	g.bank.balances[moduleAddr(types.ModuleName)] = f.bank.balances[moduleAddr(types.ModuleName)]
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	g.checkInvariants()
	reexported, err := g.keeper.ExportGenesis(g.ctx)
	require.NoError(t, err)
	require.Equal(t, exported, reexported)

	// Rebuilt indexes work: the pending units sit in carol's inbox, the
	// tree still has its child link, the tax pull is scheduled.
	inbox, err := g.query.Inbox(g.ctx, &types.QueryInboxRequest{Address: g.carol.String()})
	require.NoError(t, err)
	require.Len(t, inbox.Items, 1)
	require.Equal(t, uint64(1), inbox.Items[0].Amount)
	children, err := g.query.Children(g.ctx, &types.QueryChildrenRequest{ClassId: 2, TokenId: 1})
	require.NoError(t, err)
	require.Len(t, children.Children, 1)
	rentals, err := g.query.RentalsByUser(g.ctx, &types.QueryRentalsByUserRequest{User: g.carol.String()})
	require.NoError(t, err)
	require.Len(t, rentals.Rentals, 1)
}

func TestGenesisValidateWave34(t *testing.T) {
	f := initFixture(t)
	populateWave34(f)
	cases := []struct {
		name string
		mut  func(gs *types.GenesisState)
	}{
		{"edition supply mismatch", func(gs *types.GenesisState) { gs.Editions[0].Supply++ }},
		{"edition holders mismatch", func(gs *types.GenesisState) { gs.Editions[0].Holders = 0 }},
		{"edition in a unique class", func(gs *types.GenesisState) { gs.Editions[0].ClassId = 2 }},
		{"balance locked mismatch", func(gs *types.GenesisState) { gs.Balances[0].Locked++ }},
		{"pending units above seq", func(gs *types.GenesisState) { gs.PendingUnitSeq = 0 }},
		{"unit listing above seq", func(gs *types.GenesisState) { gs.UnitListingSeq = 0 }},
		{"recipe above class counter", func(gs *types.GenesisState) { gs.CraftRecipes[0].Id = 9 }},
		{"rental without lock", func(gs *types.GenesisState) {
			for i := range gs.Tokens {
				if gs.Tokens[i].Lock == types.TokenLock_TOKEN_LOCK_RENTED {
					gs.Tokens[i].Lock = types.TokenLock_TOKEN_LOCK_NONE
				}
			}
		}},
		{"descendants mismatch", func(gs *types.GenesisState) {
			for i := range gs.Tokens {
				if gs.Tokens[i].Descendants > 0 {
					gs.Tokens[i].Descendants++
				}
			}
		}},
		{"harberger state on a plain token", func(gs *types.GenesisState) {
			for i := range gs.Tokens {
				if gs.Tokens[i].Harberger != nil {
					gs.Tokens[i].Harberger = nil
				}
			}
		}},
		{"unknown token model", func(gs *types.GenesisState) { gs.Classes[0].TokenModel = 9 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs, err := f.keeper.ExportGenesis(f.ctx)
			require.NoError(t, err)
			tc.mut(gs)
			require.Error(t, gs.Validate())
		})
	}
}

func TestGenesisRoundTripOffersAuctions(t *testing.T) {
	f := initFixture(t)
	classID := f.createClass(f.alice)
	toks := f.mint(f.alice, classID, f.bob, 2)
	_, err := f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.carol.String(), ClassId: classID, TokenId: toks[0].TokenId, Price: spark(2), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.MakeOffer(f.ctx, &types.MsgMakeOffer{Bidder: f.dave.String(), ClassId: classID, Price: spark(1), Duration: 600})
	require.NoError(t, err)
	a, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.bob.String(), ClassId: classID, TokenId: toks[1].TokenId, ReservePrice: spark(1), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.Bid(f.ctx, &types.MsgBid{Bidder: f.carol.String(), AuctionId: a.AuctionId, Amount: spark(3)})
	require.NoError(t, err)
	f.checkInvariants()

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.Offers, 2)
	require.Len(t, exported.Auctions, 1)

	g := initFixture(t)
	g.bank.balances[moduleAddr(types.ModuleName)] = f.bank.balances[moduleAddr(types.ModuleName)]
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	g.checkInvariants()
	reexported, err := g.keeper.ExportGenesis(g.ctx)
	require.NoError(t, err)
	require.Equal(t, exported, reexported)
	offers, err := g.query.Offers(g.ctx, &types.QueryOffersRequest{ClassId: classID, TokenId: 0})
	require.NoError(t, err)
	require.Len(t, offers.Offers, 1, "the collection offer is indexed under token 0")

	bad, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	bad.AuctionSeq = 0
	require.Error(t, bad.Validate())
	bad, _ = f.keeper.ExportGenesis(f.ctx)
	for i := range bad.Tokens {
		bad.Tokens[i].Lock = types.TokenLock_TOKEN_LOCK_NONE
	}
	require.Error(t, bad.Validate(), "an auction needs its AUCTION lock")
}
