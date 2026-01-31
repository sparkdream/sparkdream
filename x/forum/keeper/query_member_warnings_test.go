package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryMemberWarnings(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberWarnings(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty member address", func(t *testing.T) {
		_, err := qs.MemberWarnings(f.ctx, &types.QueryMemberWarningsRequest{Member: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no warnings", func(t *testing.T) {
		resp, err := qs.MemberWarnings(f.ctx, &types.QueryMemberWarningsRequest{Member: testCreator})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.WarningNumber)
	})

	t.Run("has warning", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create warning for member
		warning := types.MemberWarning{
			Id:            100,
			Member:        testCreator2,
			WarningNumber: 1,
			Reason:        "First warning for spam",
			IssuedAt:      now,
		}
		f.keeper.MemberWarning.Set(f.ctx, 100, warning)

		resp, err := qs.MemberWarnings(f.ctx, &types.QueryMemberWarningsRequest{Member: testCreator2})
		require.NoError(t, err)
		require.Equal(t, uint64(1), resp.WarningNumber)
		require.Equal(t, "First warning for spam", resp.Reason)
	})
}
