package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestAuctionsBySellerFollowsAliases(t *testing.T) {
	f := initFixture(t)
	old := sdk.AccAddress([]byte("old-alice___________")).String()
	f.rep.migrated[old] = f.alice.String()
	for _, a := range []types.Auction{
		{Id: 1, ClassId: 1, TokenId: 1, Seller: f.alice.String()},
		{Id: 2, ClassId: 1, TokenId: 2, Seller: old},
		{Id: 3, ClassId: 1, TokenId: 3, Seller: f.carol.String()},
	} {
		require.NoError(t, f.keeper.Auctions.Set(f.ctx, a.Id, a))
		require.NoError(t, f.keeper.AuctionsBySeller.Set(f.ctx, collections.Join(a.Seller, a.Id)))
	}

	resp, err := f.query.AuctionsBySeller(f.ctx, &types.QueryAuctionsBySellerRequest{Seller: f.alice.String()})
	require.NoError(t, err)
	ids := []uint64{}
	for _, a := range resp.Auctions {
		ids = append(ids, a.Id)
	}
	require.ElementsMatch(t, []uint64{1, 2}, ids)

	resp, err = f.query.AuctionsBySeller(f.ctx, &types.QueryAuctionsBySellerRequest{Seller: f.bob.String()})
	require.NoError(t, err)
	require.Empty(t, resp.Auctions)
}

func TestRentalOffersFilter(t *testing.T) {
	f := initFixture(t)
	old := sdk.AccAddress([]byte("old-bob_____________")).String()
	f.rep.migrated[old] = f.bob.String()
	live := f.ctx.BlockTime().Unix() + 1000
	for _, o := range []types.RentalOffer{
		{ClassId: 1, TokenId: 1, Owner: f.alice.String(), User: f.bob.String(), ExpiresAt: live},
		{ClassId: 1, TokenId: 2, Owner: f.alice.String(), User: old, ExpiresAt: live},
		{ClassId: 1, TokenId: 3, Owner: f.alice.String(), User: "", ExpiresAt: live},
		{ClassId: 1, TokenId: 4, Owner: f.alice.String(), User: f.bob.String(), ExpiresAt: f.ctx.BlockTime().Unix()},
	} {
		require.NoError(t, f.keeper.RentalOffers.Set(f.ctx, collections.Join(o.ClassId, o.TokenId), o))
	}
	tokens := func(offers []types.RentalOffer) []uint64 {
		out := []uint64{}
		for _, o := range offers {
			out = append(out, o.TokenId)
		}
		return out
	}

	// Addressed to bob, including under his predecessor address; expired
	// offers are hidden.
	resp, err := f.query.RentalOffers(f.ctx, &types.QueryRentalOffersRequest{User: f.bob.String()})
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{1, 2}, tokens(resp.Offers))

	// No user: every unexpired offer, open ones included.
	resp, err = f.query.RentalOffers(f.ctx, &types.QueryRentalOffersRequest{})
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{1, 2, 3}, tokens(resp.Offers))
}
