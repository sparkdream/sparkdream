package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestQueries(t *testing.T) {
	f := initFixture(t)
	id := populate(f)

	p, err := f.query.Params(f.ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, f.params(), p.Params)

	classes, err := f.query.Classes(f.ctx, &types.QueryClassesRequest{})
	require.NoError(t, err)
	require.Len(t, classes.Classes, 2)

	toks, err := f.query.Tokens(f.ctx, &types.QueryTokensRequest{ClassId: id, Pagination: &query.PageRequest{Limit: 2}})
	require.NoError(t, err)
	require.Len(t, toks.Tokens, 2)
	require.NotNil(t, toks.Pagination.NextKey)

	byOwner, err := f.query.TokensByOwner(f.ctx, &types.QueryTokensByOwnerRequest{Owner: f.alice.String()})
	require.NoError(t, err)
	require.Len(t, byOwner.Tokens, 4)
	byOwner, err = f.query.TokensByOwner(f.ctx, &types.QueryTokensByOwnerRequest{Owner: f.carol.String(), ClassId: 2})
	require.NoError(t, err)
	require.Len(t, byOwner.Tokens, 2)
	byOwner, err = f.query.TokensByOwner(f.ctx, &types.QueryTokensByOwnerRequest{Owner: f.carol.String(), ClassId: id})
	require.NoError(t, err)
	require.Empty(t, byOwner.Tokens)

	owner, err := f.query.Owner(f.ctx, &types.QueryOwnerRequest{ClassId: id, TokenId: 4})
	require.NoError(t, err)
	require.Equal(t, f.alice.String(), owner.Owner)
	_, err = f.query.Owner(f.ctx, &types.QueryOwnerRequest{ClassId: id, TokenId: 99})
	require.Error(t, err)

	supply, err := f.query.Supply(f.ctx, &types.QuerySupplyRequest{ClassId: id})
	require.NoError(t, err)
	require.Equal(t, uint64(4), supply.Supply)
	require.Equal(t, uint64(1), supply.ReservedSupply)

	tok, err := f.query.Token(f.ctx, &types.QueryTokenRequest{ClassId: id, TokenId: 2})
	require.NoError(t, err)
	require.NotNil(t, tok.Listing)
	require.Equal(t, types.MetadataHash(f.token(id, 2).Metadata), tok.Token.MetadataHash)

	sellers, err := f.query.ListingsBySeller(f.ctx, &types.QueryListingsBySellerRequest{Seller: f.alice.String()})
	require.NoError(t, err)
	require.Len(t, sellers.Listings, 1)

	outbox, err := f.query.Outbox(f.ctx, &types.QueryOutboxRequest{Address: f.alice.String()})
	require.NoError(t, err)
	require.Len(t, outbox.Items, 2)

	rp, err := f.query.ReceivePolicy(f.ctx, &types.QueryReceivePolicyRequest{Address: f.dave.String()})
	require.NoError(t, err)
	require.Equal(t, types.ReceivePolicy_RECEIVE_POLICY_INBOX, rp.Policy)
	require.False(t, rp.IsDefault)
	rp, err = f.query.ReceivePolicy(f.ctx, &types.QueryReceivePolicyRequest{Address: f.bob.String()})
	require.NoError(t, err)
	require.Equal(t, types.ReceivePolicy_RECEIVE_POLICY_MEMBERS, rp.Policy)
	require.True(t, rp.IsDefault)

	po, err := f.query.PendingClassOwner(f.ctx, &types.QueryPendingClassOwnerRequest{ClassId: id})
	require.NoError(t, err)
	require.Equal(t, f.bob.String(), po.Pending.ProposedOwner)

	hr, err := f.query.HideRecordsByTarget(f.ctx, &types.QueryHideRecordsByTargetRequest{ClassId: id, TokenId: 3})
	require.NoError(t, err)
	require.Len(t, hr.Records, 1)
	one, err := f.query.HideRecord(f.ctx, &types.QueryHideRecordRequest{HideId: hr.Records[0].Id})
	require.NoError(t, err)
	require.Equal(t, hr.Records[0], one.Record)

	// Nil requests are rejected.
	_, err = f.query.Class(f.ctx, nil)
	require.Error(t, err)
}
