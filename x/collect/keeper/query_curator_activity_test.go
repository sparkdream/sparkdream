package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/collect/types"
)

func TestQueryCuratorActivity_Basic(t *testing.T) {
	f := initTestFixture(t)

	// Seed a CuratorActivity record.
	require.NoError(t, f.keeper.CuratorActivity.Set(f.ctx, f.member, types.CuratorActivity{
		Address:              f.member,
		TotalReviews:         7,
		ChallengedReviews:    2,
		UpheldReviews:        5,
		OverturnedReviews:    1,
		ConsecutiveUpheld:    3,
		ConsecutiveOverturns: 0,
	}))

	resp, err := f.queryServer.CuratorActivity(f.ctx, &types.QueryCuratorActivityRequest{
		Address: f.member,
	})
	require.NoError(t, err)
	require.Equal(t, f.member, resp.Activity.Address)
	require.Equal(t, uint64(7), resp.Activity.TotalReviews)
	require.Equal(t, uint64(2), resp.Activity.ChallengedReviews)
	require.Equal(t, uint64(5), resp.Activity.UpheldReviews)
	require.Equal(t, uint64(1), resp.Activity.OverturnedReviews)
}

func TestQueryCuratorActivity_MissingReturnsZeroedRecord(t *testing.T) {
	f := initTestFixture(t)

	// A curator who has never submitted a review: the query returns a
	// zero-valued record with the address populated (rather than NotFound).
	resp, err := f.queryServer.CuratorActivity(f.ctx, &types.QueryCuratorActivityRequest{
		Address: f.nonMember,
	})
	require.NoError(t, err)
	require.Equal(t, f.nonMember, resp.Activity.Address)
	require.Zero(t, resp.Activity.TotalReviews)
	require.Zero(t, resp.Activity.ChallengedReviews)
}

func TestQueryCuratorActivity_Validation(t *testing.T) {
	f := initTestFixture(t)

	// nil request.
	_, err := f.queryServer.CuratorActivity(f.ctx, nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// Empty address.
	_, err = f.queryServer.CuratorActivity(f.ctx, &types.QueryCuratorActivityRequest{Address: ""})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
