package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
	reptypes "sparkdream/x/rep/types"
)

// Waves 3 and 4 (docs/x-artifact-spec.md §16.2): editions and impact
// certificates, crafting, rentals, nesting, Harberger tokens and the
// NEITHER burn mode.

// --- Session mock -------------------------------------------------------------

type mockSession struct {
	bank   *mockBank
	grants map[uint64]*types.RecurringPullInfo
	fail   map[uint64]bool
	panics map[uint64]bool
	// deferred pulls fail like session's per-day ceiling (ErrTaxPullDeferred).
	deferred map[uint64]bool
	claims   []uint64
}

func newMockSession(b *mockBank) *mockSession {
	return &mockSession{bank: b, grants: map[uint64]*types.RecurringPullInfo{}, fail: map[uint64]bool{}, panics: map[uint64]bool{}, deferred: map[uint64]bool{}}
}

func (s *mockSession) RecurringPull(_ context.Context, id uint64) (types.RecurringPullInfo, bool, error) {
	g, ok := s.grants[id]
	if !ok {
		return types.RecurringPullInfo{}, false, nil
	}
	return *g, true, nil
}

func (s *mockSession) ClaimRecurringPull(_ context.Context, id uint64) (sdk.Coin, error) {
	g, ok := s.grants[id]
	if !ok || !g.Active {
		return sdk.Coin{}, fmt.Errorf("grant %d inactive", id)
	}
	if s.fail[id] {
		return sdk.Coin{}, fmt.Errorf("insufficient funds")
	}
	if s.deferred[id] {
		return sdk.Coin{}, types.ErrTaxPullDeferred.Wrap("claim would exceed max_per_epoch for the current UTC day")
	}
	if s.panics[id] {
		panic("session adapter bug")
	}
	if err := s.bank.move(g.Granter, g.Grantee, sdk.NewCoins(g.Amount)); err != nil {
		return sdk.Coin{}, err
	}
	s.claims = append(s.claims, id)
	g.NextClaim += g.PeriodSeconds
	return g.Amount, nil
}

func (f *fixture) withSession() *mockSession {
	s := newMockSession(f.bank)
	f.keeper.SetSessionKeeper(s)
	return s
}

func moduleAddress() string { return authtypes.NewModuleAddress(types.ModuleName).String() }

// --- Helpers --------------------------------------------------------------------

func (f *fixture) createEditionsClass(owner sdk.AccAddress, mut ...func(*types.MsgCreateClass)) uint64 {
	return f.createClass(owner, append([]func(*types.MsgCreateClass){func(m *types.MsgCreateClass) {
		m.TokenModel = types.TokenModel_TOKEN_MODEL_EDITIONS
	}}, mut...)...)
}

func (f *fixture) createEdition(minter sdk.AccAddress, classID uint64, maxSupply uint64, claim types.ImpactClaim) uint64 {
	f.t.Helper()
	resp, err := f.msg.CreateEdition(f.ctx, &types.MsgCreateEdition{
		Minter: minter.String(), ClassId: classID, Metadata: meta("potion"), MaxSupply: maxSupply,
		Claim: claim, AcceptedContentLicense: types.ContentLicense,
	})
	require.NoError(f.t, err)
	return resp.EditionId
}

func (f *fixture) mintUnits(minter sdk.AccAddress, classID, editionID uint64, to sdk.AccAddress, amount uint64) types.UnitResult {
	f.t.Helper()
	resp, err := f.msg.MintUnits(f.ctx, &types.MsgMintUnits{Minter: minter.String(), ClassId: classID, EditionId: editionID,
		Entries: []types.UnitMintEntry{{Recipient: to.String(), Amount: amount}}})
	require.NoError(f.t, err)
	return resp.Results[0]
}

func (f *fixture) balanceOf(classID, editionID uint64, owner sdk.AccAddress) types.Balance {
	resp, err := f.query.Balance(f.ctx, &types.QueryBalanceRequest{ClassId: classID, EditionId: editionID, Owner: owner.String()})
	if err != nil {
		return types.Balance{}
	}
	return resp.Balance.Balance
}

func (f *fixture) edition(classID, editionID uint64) types.Edition {
	f.t.Helper()
	resp, err := f.query.Edition(f.ctx, &types.QueryEditionRequest{ClassId: classID, EditionId: editionID})
	require.NoError(f.t, err)
	return resp.Edition.Edition
}

// --- NEITHER --------------------------------------------------------------------

func TestNeitherBurnMode(t *testing.T) {
	f := initFixture(t)
	neither := func(m *types.MsgCreateClass) {
		m.Flags.BurnAuthorization = types.BurnAuthorization_BURN_AUTHORIZATION_NEITHER
	}
	classID := f.createClass(f.alice, neither)

	// Self-mint: delivered, the deposit is burned instead of held.
	burnedBefore := f.bank.burned.AmountOf(bond)
	modBefore := f.moduleBalance()
	res := f.mint(f.alice, classID, f.alice, 1)
	require.True(t, res[0].Live)
	tok := f.token(classID, res[0].TokenId)
	require.True(t, tok.Deposit.IsZero())
	require.Equal(t, f.params().TokenDeposit, f.bank.burned.AmountOf(bond).Sub(burnedBefore))
	require.Equal(t, modBefore, f.moduleBalance())

	// A member recipient must still accept: the token is permanent.
	res = f.mint(f.alice, classID, f.bob, 1)
	require.False(t, res[0].Live)
	_, err := f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(),
		Entries: []types.AcceptEntry{{ClassId: classID, TokenId: res[0].TokenId}}})
	require.NoError(t, err)
	require.True(t, f.token(classID, res[0].TokenId).Deposit.IsZero())

	// Nobody may burn.
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: classID, TokenId: res[0].TokenId}}})
	require.ErrorIs(t, err, types.ErrHolderBurnNotAllowed)
	_, err = f.msg.Revoke(f.ctx, &types.MsgRevoke{Owner: f.alice.String(), ClassId: classID, TokenIds: []uint64{res[0].TokenId}, Reason: "x"})
	require.ErrorIs(t, err, types.ErrNotRevocable)

	// Transfers wait for the recipient even between members.
	tr, err := f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(),
		Entries: []types.TransferEntry{{ClassId: classID, TokenId: res[0].TokenId, Recipient: f.alice.String()}}})
	require.NoError(t, err)
	require.False(t, tr.Results[0].Delivered)

	// Shape rules.
	_, err = f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "x", Flags: types.ClassFlags{
		Transferable: true, BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_NEITHER},
		Expiry: types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_BURN, Term: 3600}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrInvalidFlags)
	f.checkInvariants()
}

// --- Editions -------------------------------------------------------------------

func TestEditionsLifecycle(t *testing.T) {
	f := initFixture(t)
	classID := f.createEditionsClass(f.alice)
	deposit := f.params().TokenDeposit

	// Unit messages refuse unique classes and vice versa.
	uniqueID := f.createClass(f.alice)
	_, err := f.msg.CreateEdition(f.ctx, &types.MsgCreateEdition{Minter: f.alice.String(), ClassId: uniqueID,
		Metadata: meta("x"), AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrWrongTokenModel)
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: classID,
		Entries: []types.MintEntry{{Metadata: meta("x")}}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrWrongTokenModel)

	editionID := f.createEdition(f.alice, classID, 100, types.ImpactClaim{})
	require.Equal(t, uint64(1), f.class(classID).Supply)

	// Mint 10 to bob (member: direct); the record costs alice one deposit.
	aliceBefore := f.bank.bal(f.alice)
	res := f.mintUnits(f.alice, classID, editionID, f.bob, 10)
	require.True(t, res.Delivered)
	require.Equal(t, deposit, aliceBefore.Sub(f.bank.bal(f.alice)))
	require.Equal(t, uint64(10), f.balanceOf(classID, editionID, f.bob).Amount)

	// A second mint to the same holder needs no new deposit.
	aliceBefore = f.bank.bal(f.alice)
	f.mintUnits(f.alice, classID, editionID, f.bob, 5)
	require.True(t, aliceBefore.Equal(f.bank.bal(f.alice)))

	// The edition cap counts units ever issued.
	_, err = f.msg.MintUnits(f.ctx, &types.MsgMintUnits{Minter: f.alice.String(), ClassId: classID, EditionId: editionID,
		Entries: []types.UnitMintEntry{{Recipient: f.bob.String(), Amount: 86}}})
	require.ErrorIs(t, err, types.ErrSupplyExceeded)

	// Bob -> dave (non-member under MEMBERS from a member: direct).
	bobBefore := f.bank.bal(f.bob)
	_, err = f.msg.TransferUnits(f.ctx, &types.MsgTransferUnits{Sender: f.bob.String(),
		Entries: []types.UnitTransferEntry{{ClassId: classID, EditionId: editionID, Recipient: f.dave.String(), Amount: 15}}})
	require.NoError(t, err)
	// Bob emptied his record (deposit back) and funded dave's (deposit out).
	require.True(t, bobBefore.Equal(f.bank.bal(f.bob)))
	require.Equal(t, uint64(0), f.balanceOf(classID, editionID, f.bob).Amount)
	require.Equal(t, uint64(15), f.balanceOf(classID, editionID, f.dave).Amount)
	require.Equal(t, uint64(1), f.edition(classID, editionID).Holders)

	// Dave (untrusted) -> bob waits in bob's inbox, units locked.
	tr, err := f.msg.TransferUnits(f.ctx, &types.MsgTransferUnits{Sender: f.dave.String(),
		Entries: []types.UnitTransferEntry{{ClassId: classID, EditionId: editionID, Recipient: f.bob.String(), Amount: 4}}})
	require.NoError(t, err)
	require.False(t, tr.Results[0].Delivered)
	pid := tr.Results[0].PendingId
	b := f.balanceOf(classID, editionID, f.dave)
	require.Equal(t, uint64(11), b.Amount)
	require.Equal(t, uint64(4), b.Locked)
	inbox, err := f.query.Inbox(f.ctx, &types.QueryInboxRequest{Address: f.bob.String()})
	require.NoError(t, err)
	require.Len(t, inbox.Items, 1)
	require.Equal(t, uint64(0), inbox.Items[0].ClassId)
	require.Equal(t, pid, inbox.Items[0].TokenId)
	require.Equal(t, uint64(4), inbox.Items[0].Amount)
	f.checkInvariants()

	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(),
		Entries: []types.AcceptEntry{{ClassId: 0, TokenId: pid}}})
	require.NoError(t, err)
	require.Equal(t, uint64(4), f.balanceOf(classID, editionID, f.bob).Amount)
	require.Equal(t, uint64(0), f.balanceOf(classID, editionID, f.dave).Locked)

	// Listing and partial buys.
	lr, err := f.msg.ListUnits(f.ctx, &types.MsgListUnits{Seller: f.dave.String(), ClassId: classID, EditionId: editionID,
		Quantity: 6, UnitPrice: spark(2), Duration: 3600})
	require.NoError(t, err)
	_, err = f.msg.BuyUnits(f.ctx, &types.MsgBuyUnits{Buyer: f.carol.String(), ListingId: lr.ListingId, Quantity: 2, ExpectedUnitPrice: spark(3)})
	require.ErrorIs(t, err, types.ErrListingChanged)
	daveBefore := f.bank.bal(f.dave)
	resp, err := f.msg.BuyUnits(f.ctx, &types.MsgBuyUnits{Buyer: f.carol.String(), ListingId: lr.ListingId, Quantity: 2, ExpectedUnitPrice: spark(2)})
	require.NoError(t, err)
	price := math.NewInt(4_000_000)
	fee := price.MulRaw(int64(f.params().SaleFeeBps)).QuoRaw(10000)
	royalty := price.MulRaw(500).QuoRaw(10000)
	require.Equal(t, fee.String(), resp.Fee)
	require.Equal(t, royalty.String(), resp.Royalty)
	require.Equal(t, price.Sub(fee).Sub(royalty), f.bank.bal(f.dave).Sub(daveBefore))
	require.Equal(t, uint64(2), f.balanceOf(classID, editionID, f.carol).Amount)
	l, err := f.query.UnitListing(f.ctx, &types.QueryUnitListingRequest{ListingId: lr.ListingId})
	require.NoError(t, err)
	require.Equal(t, uint64(4), l.Listing.Quantity)
	_, err = f.msg.DelistUnits(f.ctx, &types.MsgDelistUnits{Seller: f.dave.String(), ListingId: lr.ListingId})
	require.NoError(t, err)
	require.Equal(t, uint64(0), f.balanceOf(classID, editionID, f.dave).Locked)

	// Burn units; emptying refunds the record deposit.
	carolBefore := f.bank.bal(f.carol)
	_, err = f.msg.BurnUnits(f.ctx, &types.MsgBurnUnits{Owner: f.carol.String(), ClassId: classID, EditionId: editionID, Amount: 2})
	require.NoError(t, err)
	require.Equal(t, deposit, f.bank.bal(f.carol).Sub(carolBefore))
	e := f.edition(classID, editionID)
	require.Equal(t, uint64(2), e.Burned)
	require.Equal(t, uint64(13), e.Supply)
	f.checkInvariants()

	// Revocation needs an issuer-burnable class.
	_, err = f.msg.RevokeUnits(f.ctx, &types.MsgRevokeUnits{Owner: f.alice.String(), ClassId: classID, EditionId: editionID,
		Holder: f.dave.String(), Amount: 1, Reason: "x"})
	require.ErrorIs(t, err, types.ErrNotRevocable)
}

func TestEditionsRevokeAndHide(t *testing.T) {
	f := initFixture(t)
	classID := f.createEditionsClass(f.council, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}
		m.RoyaltyBps = 0
	})
	editionID := f.createEdition(f.council, classID, 0, types.ImpactClaim{})
	f.mintUnits(f.council, classID, editionID, f.bob, 3)
	_, err := f.msg.TransferUnits(f.ctx, &types.MsgTransferUnits{Sender: f.bob.String(),
		Entries: []types.UnitTransferEntry{{ClassId: classID, EditionId: editionID, Recipient: f.alice.String(), Amount: 1}}})
	require.ErrorIs(t, err, types.ErrNotTransferable)
	_, err = f.msg.RevokeUnits(f.ctx, &types.MsgRevokeUnits{Owner: f.council.String(), ClassId: classID, EditionId: editionID,
		Holder: f.bob.String(), Amount: 3, Reason: "misconduct"})
	require.NoError(t, err)
	require.Equal(t, uint64(0), f.edition(classID, editionID).Holders)

	// Edition hide by a council authority blanks the edition.
	_, err = f.msg.HideContent(f.ctx, &types.MsgHideContent{Authority: f.ops.String(), TargetKind: types.HideTargetKind_HIDE_TARGET_KIND_EDITION,
		ClassId: classID, TokenId: editionID, Reason: 1})
	require.NoError(t, err)
	q, err := f.query.Edition(f.ctx, &types.QueryEditionRequest{ClassId: classID, EditionId: editionID})
	require.NoError(t, err)
	require.True(t, q.Edition.Withheld)
	require.Empty(t, q.Edition.Edition.Metadata.Name)
	_, err = f.msg.MintUnits(f.ctx, &types.MsgMintUnits{Minter: f.council.String(), ClassId: classID, EditionId: editionID,
		Entries: []types.UnitMintEntry{{Recipient: f.bob.String(), Amount: 1}}})
	require.ErrorIs(t, err, types.ErrContentHidden)
	f.checkInvariants()
}

// --- Impact certificates ----------------------------------------------------------

func TestImpactCertificates(t *testing.T) {
	f := initFixture(t)
	classID := f.createEditionsClass(f.alice)

	// An initiative certificate needs a completed initiative and its assignee.
	f.rep.initiatives[7] = reptypes.Initiative{Id: 7, Assignee: f.bob.String(), Status: reptypes.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW}
	_, err := f.msg.CreateEdition(f.ctx, &types.MsgCreateEdition{Minter: f.alice.String(), ClassId: classID, Metadata: meta("cert"),
		Claim: types.ImpactClaim{Scope: "docs rewrite", InitiativeId: 7}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrInitiativeNotEligible)
	f.rep.initiatives[7] = reptypes.Initiative{Id: 7, Assignee: f.bob.String(), Status: reptypes.InitiativeStatus_INITIATIVE_STATUS_COMPLETED}
	_, err = f.msg.CreateEdition(f.ctx, &types.MsgCreateEdition{Minter: f.alice.String(), ClassId: classID, Metadata: meta("cert"),
		Claim: types.ImpactClaim{Scope: "docs rewrite", InitiativeId: 7}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrInitiativeNotEligible, "only the assignee may issue it")

	_, err = f.msg.SetMinters(f.ctx, &types.MsgSetMinters{Owner: f.alice.String(), ClassId: classID, Add: []string{f.bob.String()}})
	require.NoError(t, err)
	certID := f.createEdition(f.bob, classID, 1000, types.ImpactClaim{Scope: "docs rewrite", InitiativeId: 7, WorkStart: 1, WorkEnd: 2})
	cert := f.edition(classID, certID)
	require.Equal(t, []string{f.bob.String()}, cert.Claim.Contributors)
	_, err = f.msg.CreateEdition(f.ctx, &types.MsgCreateEdition{Minter: f.bob.String(), ClassId: classID, Metadata: meta("cert2"),
		Claim: types.ImpactClaim{Scope: "again", InitiativeId: 7}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrCertificateExists)

	// Shares: bob 600, carol 400.
	f.mintUnits(f.bob, classID, certID, f.bob, 600)
	f.mintUnits(f.bob, classID, certID, f.carol, 400)

	// Funding with nothing circulating is refused; then a retro funder pays 1000 SPARK.
	empty := f.createEdition(f.bob, classID, 0, types.ImpactClaim{})
	_, err = f.msg.FundEdition(f.ctx, &types.MsgFundEdition{Funder: f.dave.String(), ClassId: classID, EditionId: empty, Amount: spark(1)})
	require.ErrorIs(t, err, types.ErrNoCirculatingUnits)
	_, err = f.msg.FundEdition(f.ctx, &types.MsgFundEdition{Funder: f.dave.String(), ClassId: classID, EditionId: certID, Amount: sdkCoin(dream, 5)})
	require.ErrorIs(t, err, types.ErrDreamNotAccepted)
	_, err = f.msg.FundEdition(f.ctx, &types.MsgFundEdition{Funder: f.dave.String(), ClassId: classID, EditionId: certID, Amount: spark(1000)})
	require.NoError(t, err)
	f.checkInvariants()

	// Carol sells 200 to dave after the funding: the earned share stays hers.
	_, err = f.msg.TransferUnits(f.ctx, &types.MsgTransferUnits{Sender: f.carol.String(),
		Entries: []types.UnitTransferEntry{{ClassId: classID, EditionId: certID, Recipient: f.dave.String(), Amount: 200}}})
	require.NoError(t, err)
	_, err = f.msg.FundEdition(f.ctx, &types.MsgFundEdition{Funder: f.alice.String(), ClassId: classID, EditionId: certID, Amount: spark(100)})
	require.NoError(t, err)

	claim := func(who sdk.AccAddress) math.Int {
		resp, err := f.msg.ClaimEditionRewards(f.ctx, &types.MsgClaimEditionRewards{Holder: who.String(),
			Refs: []types.EditionRef{{ClassId: classID, EditionId: certID}}})
		require.NoError(t, err)
		v, ok := math.NewIntFromString(resp.Claimed)
		require.True(t, ok)
		return v
	}
	require.Equal(t, math.NewInt(660_000_000), claim(f.bob))   // 600 of 1000 + 60 of 100
	require.Equal(t, math.NewInt(420_000_000), claim(f.carol)) // 400 of 1000 + 20 of 100
	require.Equal(t, math.NewInt(20_000_000), claim(f.dave))   // 20 of 100
	require.Equal(t, math.ZeroInt(), claim(f.bob))
	f.checkInvariants()
}

// --- Crafting --------------------------------------------------------------------

func TestCrafting(t *testing.T) {
	f := initFixture(t)
	shards := f.createClass(f.alice)
	herbs := f.createEditionsClass(f.alice)
	herbID := f.createEdition(f.alice, herbs, 0, types.ImpactClaim{})
	relics := f.createClass(f.alice)

	add := func(owner sdk.AccAddress, inputs []types.CraftInput) (uint64, error) {
		resp, err := f.msg.AddCraftRecipe(f.ctx, &types.MsgAddCraftRecipe{Owner: owner.String(), ClassId: relics,
			Inputs: inputs, OutputMetadata: meta("relic"), AcceptedContentLicense: types.ContentLicense})
		if err != nil {
			return 0, err
		}
		return resp.RecipeId, nil
	}
	_, err := add(f.bob, []types.CraftInput{{ClassId: shards, Quantity: 2}})
	require.ErrorIs(t, err, types.ErrNotClassOwner)
	_, err = add(f.alice, []types.CraftInput{{ClassId: shards, Quantity: 2}, {ClassId: shards, Quantity: 1}})
	require.ErrorIs(t, err, types.ErrInvalidRecipe)
	recipe, err := add(f.alice, []types.CraftInput{{ClassId: shards, Quantity: 2}, {ClassId: herbs, EditionId: herbID, Quantity: 3}})
	require.NoError(t, err)

	r := f.mint(f.alice, shards, f.bob, 3)
	f.mintUnits(f.alice, herbs, herbID, f.bob, 5)

	craft := func(ids ...uint64) (*types.MsgCraftResponse, error) {
		refs := make([]types.TokenRef, len(ids))
		for i, id := range ids {
			refs[i] = types.TokenRef{ClassId: shards, TokenId: id}
		}
		return f.msg.Craft(f.ctx, &types.MsgCraft{Crafter: f.bob.String(), ClassId: relics, RecipeId: recipe, InputTokens: refs})
	}
	_, err = craft(r[0].TokenId)
	require.ErrorIs(t, err, types.ErrCraftInputMismatch)
	bobBefore := f.bank.bal(f.bob)
	out, err := craft(r[0].TokenId, r[1].TokenId)
	require.NoError(t, err)
	// Two shard deposits back, one relic deposit out.
	require.Equal(t, f.params().TokenDeposit, f.bank.bal(f.bob).Sub(bobBefore))
	require.Equal(t, f.bob.String(), f.token(relics, out.TokenId).Owner)
	require.Equal(t, "relic", f.token(relics, out.TokenId).Metadata.Name)
	require.Equal(t, uint64(2), f.balanceOf(herbs, herbID, f.bob).Amount)
	require.Equal(t, uint64(2), f.class(shards).Burned)

	// The recipe list is locked after the first craft; removal still works.
	_, err = add(f.alice, []types.CraftInput{{ClassId: shards, Quantity: 1}})
	require.ErrorIs(t, err, types.ErrRecipesLocked)
	_, err = f.msg.RemoveCraftRecipe(f.ctx, &types.MsgRemoveCraftRecipe{Owner: f.alice.String(), ClassId: relics, RecipeId: recipe})
	require.NoError(t, err)
	_, err = craft(r[2].TokenId)
	require.ErrorIs(t, err, types.ErrRecipeNotFound)
	f.checkInvariants()
}

// --- Rentals ---------------------------------------------------------------------

func TestRentals(t *testing.T) {
	f := initFixture(t)
	classID := f.createClass(f.alice)
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId

	offer := func(mayEnd bool) uint64 {
		resp, err := f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.bob.String(), ClassId: classID, TokenId: tok,
			Price: spark(5), Duration: 600, OwnerMayEnd: mayEnd, OfferDuration: 3600})
		require.NoError(t, err)
		return resp.Nonce
	}
	nonce := offer(false)
	_, err := f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: classID, TokenId: tok,
		ExpectedPrice: spark(5), ExpectedNonce: nonce + 1})
	require.ErrorIs(t, err, types.ErrListingChanged)
	bobBefore := f.bank.bal(f.bob)
	_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: classID, TokenId: tok,
		ExpectedPrice: spark(5), ExpectedNonce: nonce})
	require.NoError(t, err)
	s := keeperSettlement(5_000_000, f.params().SaleFeeBps, 500)
	require.Equal(t, s, f.bank.bal(f.bob).Sub(bobBefore))
	require.Equal(t, types.TokenLock_TOKEN_LOCK_RENTED, f.token(classID, tok).Lock)
	rentals, err := f.query.RentalsByUser(f.ctx, &types.QueryRentalsByUserRequest{User: f.carol.String()})
	require.NoError(t, err)
	require.Len(t, rentals.Rentals, 1)

	// The owner cannot move, list, burn or end it early.
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(),
		Entries: []types.TransferEntry{{ClassId: classID, TokenId: tok, Recipient: f.alice.String()}}})
	require.ErrorIs(t, err, types.ErrTokenLocked)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: classID, TokenId: tok}}})
	require.ErrorIs(t, err, types.ErrTokenLocked)
	_, err = f.msg.EndRental(f.ctx, &types.MsgEndRental{Signer: f.bob.String(), ClassId: classID, TokenId: tok})
	require.ErrorIs(t, err, types.ErrRentalActive)

	// The EndBlocker ends it at expiry.
	f.advance(601, 1)
	f.endBlock()
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(classID, tok).Lock)

	// An early-end offer lets the owner take it back; the renter may always return it.
	nonce = offer(true)
	_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: classID, TokenId: tok,
		ExpectedPrice: spark(5), ExpectedNonce: nonce})
	require.NoError(t, err)
	_, err = f.msg.EndRental(f.ctx, &types.MsgEndRental{Signer: f.bob.String(), ClassId: classID, TokenId: tok})
	require.NoError(t, err)

	// A transfer drops the offer.
	offer(false)
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(),
		Entries: []types.TransferEntry{{ClassId: classID, TokenId: tok, Recipient: f.alice.String()}}})
	require.NoError(t, err)
	_, err = f.query.RentalOffer(f.ctx, &types.QueryRentalOfferRequest{ClassId: classID, TokenId: tok})
	require.Error(t, err)

	// Soulbound tokens are not rentable.
	sb := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Flags.Transferable = false; m.RoyaltyBps = 0 })
	sbTok := f.mint(f.alice, sb, f.alice, 1)[0].TokenId
	_, err = f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.alice.String(), ClassId: sb, TokenId: sbTok,
		Price: spark(1), Duration: 60, OfferDuration: 60})
	require.ErrorIs(t, err, types.ErrNotRentable)
	f.checkInvariants()
}

func keeperSettlement(price int64, feeBps, royaltyBps uint32) math.Int {
	p := math.NewInt(price)
	return p.Sub(p.MulRaw(int64(feeBps)).QuoRaw(10000)).Sub(p.MulRaw(int64(royaltyBps)).QuoRaw(10000))
}

// --- Nesting ---------------------------------------------------------------------

func TestNesting(t *testing.T) {
	f := initFixture(t)
	chars := f.createClass(f.alice)
	gear := f.createClass(f.alice)
	hero := f.mint(f.alice, chars, f.bob, 1)[0].TokenId
	g := f.mint(f.alice, gear, f.bob, 3)
	sword, shield, gem := g[0].TokenId, g[1].TokenId, g[2].TokenId

	nest := func(child, parent types.TokenRef) error {
		_, err := f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.bob.String(), Child: child, Parent: parent})
		return err
	}
	heroRef := types.TokenRef{ClassId: chars, TokenId: hero}
	swordRef := types.TokenRef{ClassId: gear, TokenId: sword}
	require.NoError(t, nest(swordRef, heroRef))
	require.NoError(t, nest(types.TokenRef{ClassId: gear, TokenId: shield}, heroRef))
	require.NoError(t, nest(types.TokenRef{ClassId: gear, TokenId: gem}, swordRef))
	require.ErrorIs(t, nest(heroRef, types.TokenRef{ClassId: gear, TokenId: gem}), types.ErrNestCycle)
	require.Equal(t, uint32(3), f.token(chars, hero).Descendants)
	require.Equal(t, uint32(1), f.token(gear, sword).Descendants)
	f.checkInvariants()

	// A nested child cannot move or burn on its own.
	_, err := f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(),
		Entries: []types.TransferEntry{{ClassId: gear, TokenId: sword, Recipient: f.carol.String()}}})
	require.ErrorIs(t, err, types.ErrTokenLocked)

	// A sale pins the contents.
	lr, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: chars, TokenId: hero, Price: spark(10), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.Detach(f.ctx, &types.MsgDetach{Owner: f.bob.String(), Child: swordRef})
	require.ErrorIs(t, err, types.ErrTokenLocked, "a listed tree is frozen")
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.carol.String(), ClassId: chars, TokenId: hero, ExpectedPrice: spark(10), ExpectedNonce: lr.Nonce})
	require.ErrorIs(t, err, types.ErrContentsChanged, "an empty pin fails closed")
	children, err := f.query.Children(f.ctx, &types.QueryChildrenRequest{ClassId: chars, TokenId: hero})
	require.NoError(t, err)
	require.Len(t, children.Children, 2)
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.carol.String(), ClassId: chars, TokenId: hero, ExpectedPrice: spark(10),
		ExpectedNonce: lr.Nonce, ExpectedContentsHash: children.ContentsHash})
	require.NoError(t, err)
	for _, id := range []uint64{sword, shield, gem} {
		require.Equal(t, f.carol.String(), f.token(gear, id).Owner, "the whole tree moved")
	}
	f.checkInvariants()

	// Detach, then burning the parent releases the rest to the holder.
	_, err = f.msg.Detach(f.ctx, &types.MsgDetach{Owner: f.carol.String(), Child: types.TokenRef{ClassId: gear, TokenId: shield}})
	require.NoError(t, err)
	require.Equal(t, uint32(2), f.token(chars, hero).Descendants)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.carol.String(), Refs: []types.TokenRef{{ClassId: chars, TokenId: hero}}})
	require.NoError(t, err)
	sw := f.token(gear, sword)
	require.False(t, sw.IsNested())
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, sw.Lock)
	require.True(t, f.token(gear, gem).IsNested(), "grandchildren stay in their own parent")
	f.checkInvariants()

	// Soulbound children only go into non-transferable parents.
	sb := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Flags.Transferable = false; m.RoyaltyBps = 0 })
	badge := f.mint(f.alice, sb, f.carol, 1)[0].TokenId
	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.carol.String(), Child: types.TokenRef{ClassId: sb, TokenId: badge}, Parent: swordRef})
	require.ErrorIs(t, err, types.ErrNestNotAllowed)

	// Depth cap: sword (root) > gem is one level; a token under the gem
	// would be a second.
	f.setParams(func(p *types.Params) { p.MaxNestDepth = 1 })
	extra := f.mint(f.alice, gear, f.carol, 1)[0].TokenId
	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.carol.String(), Child: types.TokenRef{ClassId: gear, TokenId: extra},
		Parent: types.TokenRef{ClassId: gear, TokenId: gem}})
	require.ErrorIs(t, err, types.ErrNestTooDeep)
}

// --- Harberger -------------------------------------------------------------------

func TestHarberger(t *testing.T) {
	f := initFixture(t)
	sess := f.withSession()
	policy := types.HarbergerPolicy{TaxBpsPerYear: 1000, TaxPeriod: 86400, ForeclosureDuration: 864000}
	classID := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Harberger = policy })
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	price := math.NewInt(365_000_000)
	tax := policy.TaxPerPeriod(price)
	require.Equal(t, math.NewInt(100_000), tax) // 10%/yr of 365 SPARK, one day

	// Listing and rentals are not for Harberger tokens.
	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: classID, TokenId: tok, Price: spark(1), Duration: 60})
	require.ErrorIs(t, err, types.ErrHarbergerToken)

	// Inside the grace period an unpriced token is not for sale.
	_, err = f.msg.ForceBuy(f.ctx, &types.MsgForceBuy{Buyer: f.carol.String(), ClassId: classID, TokenId: tok, MaxPrice: spark(1)})
	require.ErrorIs(t, err, types.ErrNotForSale)

	start := f.ctx.BlockTime().Unix()
	sess.grants[1] = &types.RecurringPullInfo{Granter: f.bob.String(), Grantee: moduleAddress(), Active: true,
		Amount: sdk.NewCoin(bond, tax.SubRaw(1)), PeriodSeconds: 86400, NextClaim: start + 86400, ExpiresAt: start + 10*86400}
	_, err = f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.bob.String(), ClassId: classID, TokenId: tok, Price: price, GrantId: 1})
	require.ErrorIs(t, err, types.ErrInvalidHarbergerGrant, "grant below the tax")
	sess.grants[1].Amount = sdk.NewCoin(bond, tax)
	_, err = f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.bob.String(), ClassId: classID, TokenId: tok, Price: price, GrantId: 1})
	require.NoError(t, err)

	// The EndBlocker takes the tax into the community pool.
	pool := f.distr.pool.AmountOf(bond)
	f.advance(86400, 1)
	f.endBlock()
	require.Equal(t, []uint64{1}, sess.claims)
	require.Equal(t, tax, f.distr.pool.AmountOf(bond).Sub(pool))
	// The first assessment paid the mint grace period at the new price up
	// front (ART-201); the pull paid the second.
	require.Equal(t, tax.MulRaw(2), f.token(classID, tok).Harberger.TaxPaid)

	// A failed pull forecloses: the price falls linearly to zero.
	sess.fail[1] = true
	f.advance(86400, 1)
	f.endBlock()
	h := f.token(classID, tok).Harberger
	require.NotZero(t, h.ForeclosedAt)
	f.advance(432000, 1) // half the foreclosure
	q, err := f.query.HarbergerQuote(f.ctx, &types.QueryHarbergerQuoteRequest{ClassId: classID, TokenId: tok})
	require.NoError(t, err)
	require.True(t, q.Foreclosed)
	require.Equal(t, price.QuoRaw(2).String(), q.CurrentPrice)

	// Carol force-buys at the falling price; the holder gets the proceeds.
	bobBefore := f.bank.bal(f.bob)
	_, err = f.msg.ForceBuy(f.ctx, &types.MsgForceBuy{Buyer: f.carol.String(), ClassId: classID, TokenId: tok, MaxPrice: sdk.NewCoin(bond, price.QuoRaw(2).SubRaw(1))})
	require.ErrorIs(t, err, types.ErrPriceExceeded)
	_, err = f.msg.ForceBuy(f.ctx, &types.MsgForceBuy{Buyer: f.carol.String(), ClassId: classID, TokenId: tok, MaxPrice: sdk.NewCoin(bond, price)})
	require.NoError(t, err)
	half := price.QuoRaw(2)
	require.Equal(t, keeperSettlement(half.Int64(), f.params().SaleFeeBps, 500), f.bank.bal(f.bob).Sub(bobBefore))
	tk := f.token(classID, tok)
	require.Equal(t, f.carol.String(), tk.Owner)
	require.Equal(t, half, tk.Harberger.Price)
	require.Zero(t, tk.Harberger.GrantId)
	require.Zero(t, tk.Harberger.ForeclosedAt)
	f.checkInvariants()

	// Without a grant the new holder is foreclosed once the grace period ends.
	f.advance(86400, 1)
	q, err = f.query.HarbergerQuote(f.ctx, &types.QueryHarbergerQuoteRequest{ClassId: classID, TokenId: tok})
	require.NoError(t, err)
	require.True(t, q.Foreclosed)

	// Shape rules.
	_, err = f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "x", Harberger: policy,
		Flags: types.ClassFlags{}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrInvalidFlags, "harberger needs transferable")
}
