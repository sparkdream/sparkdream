package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

func TestComputeSettlement(t *testing.T) {
	for _, price := range []int64{1, 7, 99, 10_000, 123_456_789, 1_000_000_000_000_000_000} {
		for _, fee := range []uint32{0, 1, 100, 1000} {
			for _, roy := range []uint32{0, 1, 500, 2500, 9999} {
				s := keeper.ComputeSettlement(math.NewInt(price), fee, roy, 2500)
				require.Equal(t, math.NewInt(price), s.Fee.Add(s.Royalty).Add(s.Proceeds), "price %d fee %d roy %d", price, fee, roy)
				require.False(t, s.Proceeds.IsNegative())
				// Royalty clamped to the ceiling.
				require.True(t, s.Royalty.LTE(math.NewInt(price).MulRaw(2500).QuoRaw(10000)))
			}
		}
	}
}

func TestListAndBuy(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.RoyaltyBps = 500; m.RoyaltyRecipients = types.SoleRoyaltyRecipient(f.carol.String()) })
	f.mint(f.alice, id, f.bob, 1)

	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: 1, Price: spark(100), Duration: 3600})
	require.ErrorIs(t, err, types.ErrNotTokenOwner)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: sdkCoin(dream, 5), Duration: 3600})
	require.ErrorIs(t, err, types.ErrDreamNotAccepted)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: sdkCoin(bond, 0), Duration: 3600})
	require.ErrorIs(t, err, types.ErrInvalidDenom)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: spark(100), Duration: 0})
	require.ErrorIs(t, err, types.ErrInvalidMetadata)

	lresp, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: spark(100), Duration: 3600})
	require.NoError(t, err)
	require.Equal(t, uint64(1), lresp.Nonce)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_LISTED, f.token(id, 1).Lock)

	// Listed tokens cannot be transferred or listed again.
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(), Entries: []types.TransferEntry{{ClassId: id, TokenId: 1, Recipient: f.dave.String()}}})
	require.ErrorIs(t, err, types.ErrTokenLocked)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: spark(1), Duration: 3600})
	require.ErrorIs(t, err, types.ErrTokenLocked)

	// Seller raises the price: a buy signed against the old listing fails.
	uresp, err := f.msg.UpdateListing(f.ctx, &types.MsgUpdateListing{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: spark(150), Duration: 3600})
	require.NoError(t, err)
	require.Equal(t, uint64(2), uresp.Nonce)
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.dave.String(), ClassId: id, TokenId: 1, ExpectedPrice: spark(100), ExpectedNonce: 1})
	require.ErrorIs(t, err, types.ErrListingChanged)
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.dave.String(), ClassId: id, TokenId: 1, ExpectedPrice: spark(150), ExpectedNonce: 1})
	require.ErrorIs(t, err, types.ErrListingChanged)
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.bob.String(), ClassId: id, TokenId: 1, ExpectedPrice: spark(150), ExpectedNonce: 2})
	require.ErrorIs(t, err, types.ErrInvalidRecipient, "cannot buy own listing")
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.dave.String(), ClassId: id, TokenId: 1, ExpectedPrice: spark(150), ExpectedNonce: 2, ExpectedMetadataHash: "ff"})
	require.ErrorIs(t, err, types.ErrMetadataChanged)

	daveB, bobB, carolB := f.bank.bal(f.dave), f.bank.bal(f.bob), f.bank.bal(f.carol)
	resp, err := f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.dave.String(), ClassId: id, TokenId: 1, ExpectedPrice: spark(150), ExpectedNonce: 2,
		ExpectedMetadataHash: types.MetadataHash(f.token(id, 1).Metadata)})
	require.NoError(t, err)
	price := math.NewInt(150_000_000)
	fee := price.MulRaw(100).QuoRaw(10000)
	royalty := price.MulRaw(500).QuoRaw(10000)
	require.Equal(t, fee.String(), resp.Fee)
	require.Equal(t, royalty.String(), resp.Royalty)
	require.Equal(t, daveB.Sub(price), f.bank.bal(f.dave), "buyer pays exactly the price; deposit travels with the token")
	require.Equal(t, bobB.Add(price.Sub(fee).Sub(royalty)), f.bank.bal(f.bob))
	require.Equal(t, carolB.Add(royalty), f.bank.bal(f.carol))
	require.Equal(t, fee, f.distr.pool.AmountOf(bond))

	tok := f.token(id, 1)
	require.Equal(t, f.dave.String(), tok.Owner)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, tok.Lock)
	_, err = f.query.Listing(f.ctx, &types.QueryListingRequest{ClassId: id, TokenId: 1})
	require.Error(t, err)

	// A relist gets a fresh, higher nonce: an old signed buy cannot match it.
	lresp, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.dave.String(), ClassId: id, TokenId: 1, Price: spark(150), Duration: 3600})
	require.NoError(t, err)
	require.Equal(t, uint64(3), lresp.Nonce)
	f.checkInvariants()
}

func TestListingExpiryAndDelist(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.alice, 2)
	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: 1, Price: spark(5), Duration: 60})
	require.NoError(t, err)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: 2, Price: spark(5), Duration: 6000})
	require.NoError(t, err)

	all, err := f.query.Listings(f.ctx, &types.QueryListingsRequest{ClassId: id})
	require.NoError(t, err)
	require.Len(t, all.Listings, 2)

	f.advance(60, 1)
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.bob.String(), ClassId: id, TokenId: 1, ExpectedPrice: spark(5), ExpectedNonce: 1})
	require.ErrorIs(t, err, types.ErrListingExpired, "checked at use before the EndBlocker runs")
	all, err = f.query.Listings(f.ctx, &types.QueryListingsRequest{ClassId: id})
	require.NoError(t, err)
	require.Len(t, all.Listings, 1, "expired listings filtered")
	f.endBlock()
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(id, 1).Lock)

	// Delist works even with the market disabled.
	f.setParams(func(p *types.Params) { p.MarketEnabled = false })
	_, err = f.msg.Delist(f.ctx, &types.MsgDelist{Seller: f.bob.String(), ClassId: id, TokenId: 2})
	require.ErrorIs(t, err, types.ErrListingNotFound)
	_, err = f.msg.Delist(f.ctx, &types.MsgDelist{Seller: f.alice.String(), ClassId: id, TokenId: 2})
	require.NoError(t, err)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: 2, Price: spark(5), Duration: 60})
	require.ErrorIs(t, err, types.ErrMarketDisabled)
	f.checkInvariants()
}

func TestBurnListedTokenDelists(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.alice, 1)
	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: 1, Price: spark(5), Duration: 60})
	require.NoError(t, err)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.alice.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	has, err := f.keeper.Listings.Has(f.ctx, keeperPair(id, 1))
	require.NoError(t, err)
	require.False(t, has)
	f.checkInvariants()
}

func TestSplitRoyalty(t *testing.T) {
	shares := []types.RoyaltyShare{{Address: "a", WeightBps: 3333}, {Address: "b", WeightBps: 3333}, {Address: "c", WeightBps: 3334}}
	for _, r := range []int64{0, 1, 2, 7, 9999, 10000, 123_456_789} {
		parts := keeper.SplitRoyalty(math.NewInt(r), shares)
		sum := math.ZeroInt()
		for _, p := range parts {
			require.False(t, p.IsNegative())
			sum = sum.Add(p)
		}
		require.Equal(t, math.NewInt(r), sum, "royalty %d", r)
	}
	// The remainder lands on the first share.
	parts := keeper.SplitRoyalty(math.NewInt(10), shares)
	require.Equal(t, []math.Int{math.NewInt(4), math.NewInt(3), math.NewInt(3)}, parts)
}

func TestBuyPaysRoyaltySplit(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.RoyaltyBps = 500
		m.RoyaltyRecipients = []types.RoyaltyShare{{Address: f.carol.String(), WeightBps: 7000}, {Address: f.alice.String(), WeightBps: 3000}}
	})
	f.mint(f.alice, id, f.bob, 1)
	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: spark(100), Duration: 60})
	require.NoError(t, err)
	aliceB, carolB := f.bank.bal(f.alice), f.bank.bal(f.carol)
	resp, err := f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.dave.String(), ClassId: id, TokenId: 1, ExpectedPrice: spark(100), ExpectedNonce: 1})
	require.NoError(t, err)
	require.Equal(t, "5000000", resp.Royalty)
	require.Equal(t, carolB.Add(math.NewInt(3_500_000)), f.bank.bal(f.carol))
	require.Equal(t, aliceB.Add(math.NewInt(1_500_000)), f.bank.bal(f.alice))

	// The owner can re-split the royalty; a bad split is refused.
	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, UpdateMask: []string{"royalty_recipients"},
		RoyaltyRecipients: []types.RoyaltyShare{{Address: f.carol.String(), WeightBps: 9000}}})
	require.ErrorIs(t, err, types.ErrInvalidRoyaltySplit)
	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, UpdateMask: []string{"royalty_recipients"},
		RoyaltyRecipients: types.SoleRoyaltyRecipient(f.carol.String())})
	require.NoError(t, err)
	require.Equal(t, types.SoleRoyaltyRecipient(f.carol.String()), f.class(id).RoyaltyRecipients)
	f.checkInvariants()
}

func TestRoyaltyClampedToCurrentCeiling(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.RoyaltyBps = 1000; m.RoyaltyRecipients = types.SoleRoyaltyRecipient(f.carol.String()) })
	f.mint(f.alice, id, f.bob, 1)
	f.setParams(func(p *types.Params) { p.MaxRoyaltyBps = 200 })
	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: spark(100), Duration: 60})
	require.NoError(t, err)
	resp, err := f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.dave.String(), ClassId: id, TokenId: 1, ExpectedPrice: spark(100), ExpectedNonce: 1})
	require.NoError(t, err)
	require.Equal(t, "2000000", resp.Royalty)
}
