package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
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
		now := f.sdkCtx().BlockTime().Unix()

		// Create tag report
		report := types.TagReport{
			TagName:       "reported-tag",
			TotalBond:     "500",
			FirstReportAt: now,
			UnderReview:   true,
			Reporters:     []string{testCreator},
		}
		f.keeper.TagReport.Set(f.ctx, "reported-tag", report)

		resp, err := qs.TagReports(f.ctx, &types.QueryTagReportsRequest{})
		require.NoError(t, err)
		require.Equal(t, "reported-tag", resp.TagName)
	})

	t.Run("multiple reports", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create another report
		report := types.TagReport{
			TagName:       "another-tag",
			TotalBond:     "500",
			FirstReportAt: now,
			UnderReview:   false,
			Reporters:     []string{testCreator},
		}
		f.keeper.TagReport.Set(f.ctx, "another-tag", report)

		resp, err := qs.TagReports(f.ctx, &types.QueryTagReportsRequest{})
		require.NoError(t, err)
		// Should have multiple reports now
		require.NotEmpty(t, resp.TagName)
	})
}
