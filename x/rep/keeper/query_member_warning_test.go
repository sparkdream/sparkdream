package keeper_test

import (
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryMemberWarning(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	warnings := []types.MemberWarning{
		{Id: 1, Member: "member-phoenix", Reason: "rude", WarningNumber: 1, IssuedAt: 100},
		{Id: 2, Member: "member-phoenix", Reason: "spam", WarningNumber: 2, IssuedAt: 200},
		{Id: 3, Member: "member-aurora", Reason: "offtopic", WarningNumber: 1, IssuedAt: 300},
	}
	for _, w := range warnings {
		require.NoError(t, f.keeper.MemberWarning.Set(f.ctx, w.Id, w))
	}

	t.Run("get found", func(t *testing.T) {
		resp, err := qs.GetMemberWarning(f.ctx, &types.QueryGetMemberWarningRequest{Id: 2})
		require.NoError(t, err)
		require.Equal(t, uint64(2), resp.MemberWarning.Id)
		require.Equal(t, "spam", resp.MemberWarning.Reason)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := qs.GetMemberWarning(f.ctx, &types.QueryGetMemberWarningRequest{Id: 999})
		require.Error(t, err)
		require.ErrorIs(t, err, sdkerrors.ErrKeyNotFound)
	})

	t.Run("get nil request", func(t *testing.T) {
		_, err := qs.GetMemberWarning(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("list all", func(t *testing.T) {
		resp, err := qs.ListMemberWarning(f.ctx, &types.QueryAllMemberWarningRequest{
			Pagination: &query.PageRequest{CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.MemberWarning, 3)
		require.Equal(t, uint64(3), resp.Pagination.Total)
	})

	t.Run("list paginated", func(t *testing.T) {
		resp, err := qs.ListMemberWarning(f.ctx, &types.QueryAllMemberWarningRequest{
			Pagination: &query.PageRequest{Limit: 1, CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.MemberWarning, 1)
		require.NotEmpty(t, resp.Pagination.NextKey)
	})

	t.Run("list nil request", func(t *testing.T) {
		_, err := qs.ListMemberWarning(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestQueryMemberWarnings(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberWarnings(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing member", func(t *testing.T) {
		_, err := qs.MemberWarnings(f.ctx, &types.QueryMemberWarningsRequest{Member: ""})
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("returns warning for member", func(t *testing.T) {
		require.NoError(t, f.keeper.MemberWarning.Set(f.ctx, 10, types.MemberWarning{
			Id: 10, Member: "member-bravo", Reason: "test-warn", WarningNumber: 1, IssuedAt: 42,
		}))
		resp, err := qs.MemberWarnings(f.ctx, &types.QueryMemberWarningsRequest{Member: "member-bravo"})
		require.NoError(t, err)
		require.Equal(t, uint64(1), resp.WarningNumber)
		require.Equal(t, "test-warn", resp.Reason)
		require.Equal(t, int64(42), resp.IssuedAt)
	})

	t.Run("no warnings for member returns zero response", func(t *testing.T) {
		resp, err := qs.MemberWarnings(f.ctx, &types.QueryMemberWarningsRequest{Member: "no-one"})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.WarningNumber)
		require.Equal(t, "", resp.Reason)
	})
}
