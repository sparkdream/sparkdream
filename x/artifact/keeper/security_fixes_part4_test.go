package keeper_test

import (
	"encoding/hex"
	"testing"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

// Regression tests for the x/artifact findings of the 2026-11-04 review
// (ART-201 .. ART-207).

// eventAttr returns the values of attribute key on every event of type typ
// the context saw.
func eventAttr(ctx sdk.Context, typ, key string) []string {
	var out []string
	for _, e := range ctx.EventManager().Events() {
		if e.Type != typ {
			continue
		}
		for _, a := range e.Attributes {
			if a.Key == key {
				out = append(out, a.Value)
			}
		}
	}
	return out
}

type harbergerRig struct {
	f       *fixture
	sess    *mockSession
	policy  types.HarbergerPolicy
	classID uint64
	tok     uint64
	nextID  uint64
}

func newHarbergerRig(t *testing.T) *harbergerRig {
	f := initFixture(t)
	r := &harbergerRig{f: f, sess: f.withSession(),
		policy: types.HarbergerPolicy{TaxBpsPerYear: 1000, TaxPeriod: 86400, ForeclosureDuration: 864000}}
	r.classID = f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Harberger = r.policy })
	r.tok = f.mint(f.alice, r.classID, f.bob, 1)[0].TokenId
	return r
}

// grant registers a fresh RecurringPull from bob paying amount per period,
// first pulled at firstPull, and returns its id.
func (r *harbergerRig) grant(amount sdkmath.Int, firstPull int64) uint64 {
	r.nextID++
	now := r.f.ctx.BlockTime().Unix()
	r.sess.grants[r.nextID] = &types.RecurringPullInfo{Granter: r.f.bob.String(), Grantee: moduleAddress(), Active: true,
		Amount: sdk.NewCoin(bond, amount), PeriodSeconds: r.policy.TaxPeriod, NextClaim: firstPull, ExpiresAt: now + 400*86400}
	return r.nextID
}

func (r *harbergerRig) setPrice(price sdkmath.Int, grantID uint64) error {
	_, err := r.f.msg.SetHarbergerPrice(r.f.ctx, &types.MsgSetHarbergerPrice{Owner: r.f.bob.String(), ClassId: r.classID, TokenId: r.tok, Price: price, GrantId: grantID})
	return err
}

func (r *harbergerRig) state() *types.HarbergerState { return r.f.token(r.classID, r.tok).Harberger }

// ART-201: swapping the price to 1 uspark and straight back, each with a
// fresh grant, used to move the due date one period for 1 uspark, because
// the skipped time was priced by the holder. It is now charged at the price
// actually held, so ten such periods pay ten periods of tax.
func TestHarbergerPriceSwapPaysTheHeldPrice(t *testing.T) {
	r := newHarbergerRig(t)
	f := r.f
	high := sdkmath.NewInt(365_000_000)
	tax := r.policy.TaxPerPeriod(high)
	now := func() int64 { return f.ctx.BlockTime().Unix() }

	require.NoError(t, r.setPrice(high, r.grant(tax, now()+86400)))
	pool := f.distr.pool.AmountOf(bond)
	for i := 0; i < 10; i++ {
		f.advance(86000, 1)
		f.endBlock()
		require.NoError(t, r.setPrice(sdkmath.OneInt(), r.grant(sdkmath.OneInt(), now()+86400)))
		require.NoError(t, r.setPrice(high, r.grant(tax, now()+86400)))
	}
	require.Empty(t, r.sess.claims, "no pull ever fell due")
	paid := f.distr.pool.AmountOf(bond).Sub(pool)
	honest := r.policy.TaxFor(high, 86000).MulRaw(10)
	require.True(t, paid.GTE(honest), "paid %s for ten periods, honest tax %s", paid, honest)
	h := r.state()
	require.Equal(t, high, h.Price)
	require.Equal(t, high, h.PaidPrice)
	require.Zero(t, h.ForeclosedAt)
	f.checkInvariants()
}

// ART-201: lowering the price just before a pull so the pull pays 1 uspark,
// then raising it again inside the period that pull covered, charges the
// difference for the rest of that period.
func TestHarbergerRaiseInsidePaidWindowChargesTheDifference(t *testing.T) {
	r := newHarbergerRig(t)
	f := r.f
	high := sdkmath.NewInt(365_000_000)
	tax := r.policy.TaxPerPeriod(high)
	now := func() int64 { return f.ctx.BlockTime().Unix() }

	require.NoError(t, r.setPrice(high, r.grant(tax, now()+86400)))
	due := r.state().NextDue
	f.advance(86300, 1)
	// Same due date, 1 uspark grant: nothing skipped, nothing charged.
	before := r.state().TaxPaid
	require.NoError(t, r.setPrice(sdkmath.OneInt(), r.grant(sdkmath.OneInt(), due)))
	require.Equal(t, before, r.state().TaxPaid)
	require.Equal(t, high, r.state().PaidPrice, "the window already paid stays assessed at the high price")

	f.advance(100, 1)
	f.endBlock()
	require.Len(t, r.sess.claims, 1)
	h := r.state()
	require.Equal(t, sdkmath.OneInt(), h.PaidPrice, "the pull paid the new period at 1 uspark")
	require.Equal(t, due+86400, h.NextDue)

	// Raising back to the high price for the period just paid at 1 uspark
	// costs the difference up front.
	before = h.TaxPaid
	require.NoError(t, r.setPrice(high, r.grant(tax, h.NextDue)))
	h = r.state()
	owed := r.policy.TaxFor(high.SubRaw(1), 86400)
	require.Equal(t, before.Add(owed), h.TaxPaid)
	require.Equal(t, high, h.PaidPrice)
	// A second raise to the same price inside the window costs nothing.
	require.NoError(t, r.setPrice(high, r.grant(tax, h.NextDue)))
	require.Equal(t, before.Add(owed), r.state().TaxPaid)
	f.checkInvariants()
}

// ART-201: the mint grace period is paid at the first assessed price, and a
// paid arrival's grace is free only at the price paid.
func TestHarbergerGraceAssessedAtArrivalPrice(t *testing.T) {
	r := newHarbergerRig(t)
	f := r.f
	high := sdkmath.NewInt(365_000_000)
	tax := r.policy.TaxPerPeriod(high)
	now := func() int64 { return f.ctx.BlockTime().Unix() }

	require.Equal(t, sdkmath.ZeroInt(), r.state().PaidPrice, "a mint arrives at price 0")
	require.NoError(t, r.setPrice(high, r.grant(tax, now()+86400)))
	require.Equal(t, tax, r.state().TaxPaid, "the grace period is charged at the assessed price")

	// Carol buys at the assessed price: her grace is paid at that price,
	// so re-assessing at the same price costs nothing, doubling it costs
	// the difference.
	_, err := f.msg.ForceBuy(f.ctx, &types.MsgForceBuy{Buyer: f.carol.String(), ClassId: r.classID, TokenId: r.tok, MaxPrice: sdk.NewCoin(bond, high)})
	require.NoError(t, err)
	h := r.state()
	require.Equal(t, high, h.PaidPrice)
	require.True(t, h.TaxPaid.IsZero())
	carolGrant := func(amount sdkmath.Int) uint64 {
		id := r.grant(amount, now()+86400)
		r.sess.grants[id].Granter = f.carol.String()
		return id
	}
	_, err = f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.carol.String(), ClassId: r.classID, TokenId: r.tok, Price: high, GrantId: carolGrant(tax)})
	require.NoError(t, err)
	require.True(t, r.state().TaxPaid.IsZero(), "grace at the price paid is free")
	double := high.MulRaw(2)
	_, err = f.msg.SetHarbergerPrice(f.ctx, &types.MsgSetHarbergerPrice{Owner: f.carol.String(), ClassId: r.classID, TokenId: r.tok, Price: double, GrantId: carolGrant(r.policy.TaxPerPeriod(double))})
	require.NoError(t, err)
	require.Equal(t, r.policy.TaxFor(high, 86400), r.state().TaxPaid, "the raise pays the difference for the rest of the grace")
	f.checkInvariants()
}

// ART-204: a grant whose per-day ceiling cannot take two pulls is refused,
// and a pull session defers (its per-day ceiling) is retried when the UTC
// day turns instead of foreclosing a paying holder.
func TestHarbergerDeferredPullDoesNotForeclose(t *testing.T) {
	r := newHarbergerRig(t)
	f := r.f
	high := sdkmath.NewInt(365_000_000)
	tax := r.policy.TaxPerPeriod(high)
	now := f.ctx.BlockTime().Unix()

	tight := r.grant(tax, now+86400)
	r.sess.grants[tight].MaxPerEpoch = tax.MulRaw(2).SubRaw(1)
	require.ErrorIs(t, r.setPrice(high, tight), types.ErrInvalidHarbergerGrant, "max_per_epoch must cover two pulls")
	ok := r.grant(tax, now+86400)
	r.sess.grants[ok].MaxPerEpoch = tax.MulRaw(2)
	require.NoError(t, r.setPrice(high, ok))
	due := r.state().NextDue

	r.sess.deferred[ok] = true
	f.advance(86400, 1)
	f.endBlock()
	h := r.state()
	require.Zero(t, h.ForeclosedAt, "a deferred pull does not foreclose")
	require.Equal(t, ok, h.GrantId, "the grant stays")
	require.Equal(t, (due/86400+1)*86400, h.NextDue, "retried when the UTC day turns")
	has, err := f.keeper.HarbergerDue.Has(f.ctx, collections.Join3(h.NextDue, r.classID, r.tok))
	require.NoError(t, err)
	require.True(t, has)
	require.True(t, hasEvent(f.ctx, types.EventHarbergerTaxDeferred))
	require.Empty(t, r.sess.claims)

	r.sess.deferred[ok] = false
	f.advance(h.NextDue-f.ctx.BlockTime().Unix(), 1)
	f.endBlock()
	require.Equal(t, []uint64{ok}, r.sess.claims, "the pull is taken once the day turns")
	require.Zero(t, r.state().ForeclosedAt)
	f.checkInvariants()
}

// ART-205: a seed revealed after the class deadline does not take the
// holders' refund: the release queue pays the escrowed prices back to them,
// and a holder burning in between gets the same.
func TestLateSeedRevealRefundsHolders(t *testing.T) {
	f := initFixture(t)
	seed := []byte("aurora-late")
	deadline := f.ctx.BlockTime().Unix() + types.MinRevealWindow
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(2)}
		m.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: types.SeedCommitment(seed), Deadline: deadline}
		m.PayoutAddress = f.carol.String()
	})
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 2, MaxPrice: spark(2)})
	require.NoError(t, err)
	carolB, bobB := f.bank.bal(f.carol), f.bank.bal(f.bob)

	f.advance(types.MinRevealWindow, 1)
	_, err = f.msg.RevealSeed(f.ctx, &types.MsgRevealSeed{Owner: f.alice.String(), ClassId: id, Seed: hex.EncodeToString(seed)})
	require.NoError(t, err, "a late reveal is allowed")
	require.Equal(t, []string{"true"}, eventAttr(f.ctx, types.EventSeedRevealed, "late"))
	require.Equal(t, types.TokenSeed(seed, id, 1), f.tokenQuery(id, 1).Seed)

	// Burning before the queue visits the token refunds price and deposit.
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	require.Equal(t, bobB.Add(spark(2).Amount).Add(f.params().TokenDeposit), f.bank.bal(f.bob))

	f.advance(1, 1)
	f.endBlock()
	require.True(t, f.token(id, 2).Paid.IsZero())
	require.Equal(t, bobB.Add(spark(4).Amount).Add(f.params().TokenDeposit), f.bank.bal(f.bob), "the queue refunds the holder")
	require.Equal(t, carolB, f.bank.bal(f.carol), "the class gets nothing from a late reveal")
	require.True(t, hasEvent(f.ctx, types.EventPaymentRefunded))
	require.Equal(t, f.params().TokenDeposit, f.moduleBalance())
	f.checkInvariants()
}

// ART-205: a blind token revealed after the deadline refunds its escrowed
// price to the holder instead of releasing it to the class.
func TestLateBlindRevealRefundsHolder(t *testing.T) {
	f := initFixture(t)
	root, salts, metas, proofs := f.blindTree(1, 2)
	deadline := f.ctx.BlockTime().Unix() + types.MinRevealWindow
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(2)}
		m.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_BLIND_METADATA, Commitment: hex.EncodeToString(root), Deadline: deadline}
		m.PayoutAddress = f.carol.String()
	})
	require.Equal(t, uint64(1), id)
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 2, MaxPrice: spark(2)})
	require.NoError(t, err)
	carolB, bobB := f.bank.bal(f.carol), f.bank.bal(f.bob)
	reveal := func(tokenID uint64) error {
		_, err := f.msg.Reveal(f.ctx, &types.MsgReveal{Signer: f.alice.String(), ClassId: id, TokenId: tokenID, Metadata: metas[tokenID-1],
			Salt: hex.EncodeToString(salts[tokenID-1]), Proof: types.HexHashes(proofs[tokenID-1])})
		return err
	}
	// In time: the class is paid.
	require.NoError(t, reveal(1))
	fee := spark(2).Amount.MulRaw(int64(f.params().SaleFeeBps)).QuoRaw(10000)
	require.Equal(t, carolB.Add(spark(2).Amount.Sub(fee)), f.bank.bal(f.carol))

	// Late: the holder is refunded and keeps the revealed token.
	f.advance(types.MinRevealWindow, 1)
	require.NoError(t, reveal(2))
	tok := f.token(id, 2)
	require.False(t, tok.Unrevealed)
	require.Equal(t, metas[1], tok.Metadata)
	require.True(t, tok.Paid.IsZero())
	require.Equal(t, bobB.Add(spark(2).Amount), f.bank.bal(f.bob))
	require.Equal(t, carolB.Add(spark(2).Amount.Sub(fee)), f.bank.bal(f.carol), "nothing more for the class")
	require.Equal(t, f.params().TokenDeposit.MulRaw(2), f.moduleBalance())
	f.checkInvariants()
}

// ART-206: the burn event names who the deposit went to when a session key
// paid it.
func TestBurnEventNamesTheRefundee(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	resp, err := f.msg.Mint(f.sessionCtx(f.alice), &types.MsgMint{
		Minter: f.alice.String(), ClassId: id, AcceptedContentLicense: types.ContentLicense,
		Entries: []types.MintEntry{{Recipient: f.dave.String(), Metadata: meta("a")}},
	})
	require.NoError(t, err)
	plain := f.mint(f.alice, id, f.dave, 1)[0].TokenId
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.dave.String(), Refs: []types.TokenRef{
		{ClassId: id, TokenId: resp.Results[0].TokenId}, {ClassId: id, TokenId: plain}}})
	require.NoError(t, err)
	require.Equal(t, []string{f.alice.String(), f.dave.String()}, eventAttr(f.ctx, types.EventBurned, "refund_to"))
}

// ART-207: a child that would burn before a rented tree's rental ends
// cannot be nested into it.
func TestNestIntoRentedTreeRespectsTheRental(t *testing.T) {
	f := initFixture(t)
	loadouts := f.createClass(f.alice)
	consumables := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Expiry = types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_BURN, Term: 1000}
	})
	root := f.mint(f.alice, loadouts, f.bob, 1)[0].TokenId
	potion := f.mint(f.alice, consumables, f.bob, 1)[0].TokenId
	gem := f.mint(f.alice, loadouts, f.bob, 1)[0].TokenId
	offer, err := f.msg.OfferRental(f.ctx, &types.MsgOfferRental{Owner: f.bob.String(), ClassId: loadouts, TokenId: root,
		Price: spark(0), Duration: 2000, OfferDuration: 60})
	require.NoError(t, err)
	_, err = f.msg.AcceptRental(f.ctx, &types.MsgAcceptRental{Renter: f.carol.String(), ClassId: loadouts, TokenId: root,
		ExpectedPrice: spark(0), ExpectedNonce: offer.Nonce})
	require.NoError(t, err)

	nest := func(child uint64, class uint64) error {
		_, err := f.msg.Nest(f.ctx, &types.MsgNest{Owner: f.bob.String(), Child: types.TokenRef{ClassId: class, TokenId: child},
			Parent: types.TokenRef{ClassId: loadouts, TokenId: root}})
		return err
	}
	require.ErrorIs(t, nest(potion, consumables), types.ErrNestNotAllowed, "the potion would burn mid-rental")
	require.NoError(t, nest(gem, loadouts), "a lasting child is fine")
	f.checkInvariants()
}
