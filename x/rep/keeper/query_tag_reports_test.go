package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryTagReports(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.TagReports(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no reports", func(t *testing.T) {
		resp, err := qs.TagReports(f.ctx, &types.QueryTagReportsRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.TagName)
	})

	t.Run("has reports", func(t *testing.T) {
		require.NoError(t, f.keeper.TagReport.Set(f.ctx, "reported-tag", types.TagReport{
			TagName:       "reported-tag",
			TotalBond:     "500",
			FirstReportAt: f.ctx.BlockTime().Unix(),
			UnderReview:   true,
			Reporters:     []string{"addr1"},
		}))

		resp, err := qs.TagReports(f.ctx, &types.QueryTagReportsRequest{})
		require.NoError(t, err)
		require.NotEmpty(t, resp.TagName)
	})
}
