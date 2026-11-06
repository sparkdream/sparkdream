package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
	commontypes "sparkdream/x/common/types"
)

// Regression tests for the x/artifact findings of the 2026-11-01 review
// (docs/untracked/module-security-completeness-review-2026-11-01.md, Part 3).

// ART-101: swapping in a fresh tax grant before each due date used to push
// the first pull out one period every time, so no tax was ever paid. The
// gap a replacement grant leaves is now charged up front.
func TestHarbergerGrantRolloverStillPays(t *testing.T) {
	f := initFixture(t)
	sess := f.withSession()
	policy := types.HarbergerPolicy{TaxBpsPerYear: 1000, TaxPeriod: 86400, ForeclosureDuration: 864000}
	classID := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Harberger = policy })
	tok := f.mint(f.alice, classID, f.bob, 1)[0].TokenId
	price := sdkmath.NewInt(365_000_000)
	tax := policy.TaxPerPeriod(price)
	grant := func(id uint64, firstPull int64) {
		now := f.ctx.BlockTime().Unix()
		sess.grants[id] = &types.RecurringPullInfo{Granter: f.bob.String(), Grantee: moduleAddress(), Active: true,
			Amount: sdk.NewCoin(bond, tax), PeriodSeconds: 86400, NextClaim: firstPull, ExpiresAt: now + 400*86400}
	}

	// A grant whose first pull lies beyond one tax period is refused.
	grant(99, f.ctx.BlockTime().Unix()+360*86400)
	_, err := f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.bob.String(), ClassId: classID, TokenId: tok, Price: price, GrantId: 99})
	require.ErrorIs(t, err, types.ErrInvalidHarbergerGrant)

	pool := f.distr.pool.AmountOf(bond)
	for i := uint64(1); i <= 5; i++ {
		grant(i, f.ctx.BlockTime().Unix()+86400)
		_, err := f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.bob.String(), ClassId: classID, TokenId: tok, Price: price, GrantId: i})
		require.NoError(t, err)
		delete(sess.grants, i-1)
		f.advance(86000, 1)
		f.endBlock()
	}
	require.Empty(t, sess.claims, "no pull ever fell due")
	// The first grant fit the grace period, which the token arrived in at
	// price 0 and so was paid up front at the assessed price (ART-201);
	// each of the four swaps paid the 86,000 s it pushed the due date out.
	owed := tax.Add(policy.TaxFor(price, 86000).MulRaw(4))
	h := f.token(classID, tok).Harberger
	require.Equal(t, owed, h.TaxPaid)
	require.Equal(t, owed, f.distr.pool.AmountOf(bond).Sub(pool))
	require.True(t, owed.GT(tax.MulRaw(4)), "about one period per swap")

	// Curing a foreclosure pays the first period up front.
	sess.fail[5] = true
	f.advance(1000, 1)
	f.endBlock()
	require.NotZero(t, f.token(classID, tok).Harberger.ForeclosedAt)
	f.advance(5000, 1)
	grant(6, f.ctx.BlockTime().Unix()+86400)
	_, err = f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.bob.String(), ClassId: classID, TokenId: tok, Price: price, GrantId: 6})
	require.NoError(t, err)
	h = f.token(classID, tok).Harberger
	require.Zero(t, h.ForeclosedAt)
	require.Equal(t, owed.Add(tax), h.TaxPaid)
	f.checkInvariants()
}

// ART-101: a gift no longer restarts the grace period, so two holders
// passing a token back and forth never escape the tax.
func TestHarbergerGiftCarriesTheObligation(t *testing.T) {
	f := initFixture(t)
	policy := types.HarbergerPolicy{TaxBpsPerYear: 1000, TaxPeriod: 1000, ForeclosureDuration: 10000}
	classID := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Harberger = policy
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(10)}
	})
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: classID, Quantity: 1, MaxPrice: spark(10)})
	require.NoError(t, err)
	grace := f.token(classID, 1).Harberger.GraceUntil

	from, to := f.bob, f.carol
	for i := 0; i < 4; i++ {
		f.advance(900, 1)
		_, err := f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: from.String(),
			Entries: []types.TransferEntry{{ClassId: classID, TokenId: 1, Recipient: to.String()}}})
		require.NoError(t, err)
		require.Equal(t, grace, f.token(classID, 1).Harberger.GraceUntil, "gift %d kept the grace end", i)
		from, to = to, from
	}
	q, err := f.query.HarbergerQuote(f.ctx, &types.QueryHarbergerQuoteRequest{ClassId: classID, TokenId: 1})
	require.NoError(t, err)
	require.True(t, q.Foreclosed, "the ping-pong ran past the original grace")
	f.checkInvariants()
}

// ART-102: records a migrated member's successor creates name the token's
// stored owner, so exported genesis validates, the invariants hold, the
// rental can be taken up and a nested tree keeps one owner.
func TestSuccessorRecordsNameTheStoredOwner(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 5)
	f.migrate(f.bob.String(), f.dave.String())
	box := f.mint(f.alice, id, f.dave, 1)[0].TokenId

	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.dave.String(), ClassId: id, TokenId: 1, Price: spark(1), Duration: 3600})
	require.NoError(t, err)
	l, err := f.keeper.Listings.Get(f.ctx, collections.Join(id, uint64(1)))
	require.NoError(t, err)
	require.Equal(t, f.bob.String(), l.Seller)
	a, err := f.msg.StartAuction(f.ctx, &types.MsgStartAuction{Seller: f.dave.String(), ClassId: id, TokenId: 2, ReservePrice: spark(1), Duration: 600})
	require.NoError(t, err)
	au, err := f.keeper.Auctions.Get(f.ctx, a.AuctionId)
	require.NoError(t, err)
	require.Equal(t, f.bob.String(), au.Seller)

	offer, err := f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.dave.String(), ClassId: id, TokenId: 3,
		Price: spark(1), Duration: 600, OfferDuration: 600})
	require.NoError(t, err)
	_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: id, TokenId: 3,
		ExpectedPrice: spark(1), ExpectedNonce: offer.Nonce})
	require.NoError(t, err, "the successor's offer can be taken up")

	// Nesting the predecessor's token in the successor's re-keys the child
	// to the tree's owner.
	_, err = f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.dave.String(), Child: types.TokenRef{ClassId: id, TokenId: 4},
		Parent: types.TokenRef{ClassId: id, TokenId: box}})
	require.NoError(t, err)
	require.Equal(t, f.dave.String(), f.token(id, 4).Owner)
	has, err := f.keeper.TokensByOwner.Has(f.ctx, collections.Join3(f.bob.String(), id, uint64(4)))
	require.NoError(t, err)
	require.False(t, has)

	// A pending (inbox) transfer names the stored owner too, so the record,
	// the lock invariant and genesis validation agree (ART-202); the
	// successor can still cancel it.
	zenith := addr("zenith")
	_, err = f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: zenith.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.dave.String(),
		Entries: []types.TransferEntry{{ClassId: id, TokenId: 5, Recipient: zenith.String()}}})
	require.NoError(t, err)
	pt, err := f.keeper.PendingTransfers.Get(f.ctx, collections.Join(id, uint64(5)))
	require.NoError(t, err)
	require.Equal(t, f.bob.String(), pt.From)
	has, err = f.keeper.OutboxBySender.Has(f.ctx, collections.Join3(f.bob.String(), id, uint64(5)))
	require.NoError(t, err)
	require.True(t, has, "outbox indexed by the stored owner")
	f.checkInvariants()
	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, gs.Validate())
	_, err = f.msg.CancelOutgoing(f.ctx, &types.MsgCancelOutgoing{Signer: f.dave.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 5}}})
	require.NoError(t, err)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(id, 5).Lock)

	f.checkInvariants()
	gs, err = f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, gs.Validate())
}

// ART-105: InitGenesis recomputes media labels instead of trusting the file.
func TestInitGenesisRelabelsMedia(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 1) // meta() carries an external uri
	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	for i := range gs.Tokens {
		gs.Tokens[i].MediaFlags, gs.Tokens[i].MediaRulesVersion = 0, 1
	}
	for i := range gs.Classes {
		gs.Classes[i].MediaFlags, gs.Classes[i].MediaRulesVersion = 7, 1
	}

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gs))
	tok := g.token(id, 1)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_EXTERNAL_URI), tok.MediaFlags)
	require.Equal(t, commontypes.MediaRulesVersion, tok.MediaRulesVersion)
	c, err := g.keeper.Classes.Get(g.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.LabelClass(c), c.MediaFlags)
	require.Equal(t, commontypes.MediaRulesVersion, c.MediaRulesVersion)
}

// ART-108: a class drain reads at most max_expirations_per_block queued
// classes per block, even when every queued class is already exhausted.
func TestDrainQueueReadsAtMostTheBudget(t *testing.T) {
	f := initFixture(t)
	f.setParams(func(p *types.Params) { p.MaxExpirationsPerBlock = 2 })
	for i := 0; i < 5; i++ {
		require.NoError(t, f.keeper.ClassScrubQueue.Set(f.ctx, f.createClass(f.alice), 0))
	}
	remaining := func() int {
		n := 0
		require.NoError(t, f.keeper.ClassScrubQueue.Walk(f.ctx, nil, func(uint64, uint64) (bool, error) { n++; return false, nil }))
		return n
	}
	f.endBlock()
	require.Equal(t, 3, remaining())
	f.endBlock()
	f.endBlock()
	require.Zero(t, remaining())
}

// ART-106: a rental must not outlast anything nested in the rented tree.
func TestRentalBoundedByNestedExpiry(t *testing.T) {
	f := initFixture(t)
	loadouts := f.createClass(f.alice)
	consumables := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Expiry = types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_BURN, Term: 1000}
	})
	root := f.mint(f.alice, loadouts, f.bob, 1)[0].TokenId
	potion := f.mint(f.alice, consumables, f.bob, 1)[0].TokenId
	_, err := f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.bob.String(), Child: types.TokenRef{ClassId: consumables, TokenId: potion},
		Parent: types.TokenRef{ClassId: loadouts, TokenId: root}})
	require.NoError(t, err)
	hash, err := f.keeper.ContentsHash(f.ctx, f.token(loadouts, root))
	require.NoError(t, err)
	accept := func(duration int64) error {
		offer, err := f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.bob.String(), ClassId: loadouts, TokenId: root,
			Price: spark(0), Duration: duration, OfferDuration: 60})
		require.NoError(t, err)
		_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: loadouts, TokenId: root,
			ExpectedPrice: spark(0), ExpectedNonce: offer.Nonce, ExpectedContentsHash: hash})
		return err
	}
	require.ErrorIs(t, accept(2000), types.ErrNotRentable, "the potion would burn mid-rental")
	require.NoError(t, accept(900))
	f.checkInvariants()
}

// ART-107: a tombstoned signer resolves to no balance, not its own.
func TestTombstoneCannotSpendUnits(t *testing.T) {
	f := initFixture(t)
	cls := f.createEditionsClass(f.alice, func(m *types.MsgCreateClass) { m.Flags.Transferable = true })
	ed := f.createEdition(f.alice, cls, 0, types.ImpactClaim{})
	f.mintUnits(f.alice, cls, ed, f.alice, 5)
	f.migrate(f.alice.String(), f.dave.String())

	_, err := f.msg.BurnUnits(f.ctx, &types.MsgBurnUnits{Owner: f.alice.String(), ClassId: cls, EditionId: ed, Amount: 1})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = f.msg.BurnUnits(f.ctx, &types.MsgBurnUnits{Owner: f.dave.String(), ClassId: cls, EditionId: ed, Amount: 1})
	require.NoError(t, err, "the successor spends the predecessor's balance")
	require.Equal(t, uint64(4), f.balanceOf(cls, ed, f.alice).Amount)
	f.checkInvariants()
}
