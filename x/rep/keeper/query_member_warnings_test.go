package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryMemberWarnings_EmptyStore(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.MemberWarnings(f.ctx, &types.QueryMemberWarningsRequest{Member: "someone"})
	require.NoError(t, err)
	require.Zero(t, resp.WarningNumber)
	require.Empty(t, resp.Reason)
}

func TestQueryMemberWarnings_FindsMatchingMember(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	require.NoError(t, f.keeper.MemberWarning.Set(f.ctx, 1, types.MemberWarning{
		Id:            1,
		Member:        "alice",
		WarningNumber: 2,
		Reason:        "spamming",
		IssuedAt:      42,
	}))
	require.NoError(t, f.keeper.MemberWarning.Set(f.ctx, 2, types.MemberWarning{
		Id:            2,
		Member:        "bob",
		WarningNumber: 1,
		Reason:        "unrelated",
	}))

	resp, err := qs.MemberWarnings(f.ctx, &types.QueryMemberWarningsRequest{Member: "alice"})
	require.NoError(t, err)
	require.Equal(t, uint64(2), resp.WarningNumber)
	require.Equal(t, "spamming", resp.Reason)
	require.Equal(t, int64(42), resp.IssuedAt)
}

func TestQueryMemberWarnings_InvalidRequests(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.MemberWarnings(f.ctx, nil)
	require.Error(t, err)

	_, err = qs.MemberWarnings(f.ctx, &types.QueryMemberWarningsRequest{Member: ""})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
