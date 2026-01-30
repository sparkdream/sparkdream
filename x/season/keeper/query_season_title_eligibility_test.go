package keeper_test

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func createNSeasonTitleEligibility(keeper keeper.Keeper, ctx context.Context, n int) []types.SeasonTitleEligibility {
	items := make([]types.SeasonTitleEligibility, n)
	for i := range items {
		items[i].TitleSeason = uint64(i)
		items[i].Season = uint64(i)
		_ = keeper.SeasonTitleEligibility.Set(ctx, items[i].TitleSeason, items[i])
	}
	return items
}

func TestSeasonTitleEligibilityQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNSeasonTitleEligibility(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetSeasonTitleEligibilityRequest
		response *types.QueryGetSeasonTitleEligibilityResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetSeasonTitleEligibilityRequest{
				TitleSeason: msgs[0].TitleSeason,
			},
			response: &types.QueryGetSeasonTitleEligibilityResponse{SeasonTitleEligibility: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetSeasonTitleEligibilityRequest{
				TitleSeason: msgs[1].TitleSeason,
			},
			response: &types.QueryGetSeasonTitleEligibilityResponse{SeasonTitleEligibility: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetSeasonTitleEligibilityRequest{
				TitleSeason: 100000,
			},
			err: status.Error(codes.NotFound, "not found"),
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetSeasonTitleEligibility(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestSeasonTitleEligibilityQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNSeasonTitleEligibility(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllSeasonTitleEligibilityRequest {
		return &types.QueryAllSeasonTitleEligibilityRequest{
			Pagination: &query.PageRequest{
				Key:        next,
				Offset:     offset,
				Limit:      limit,
				CountTotal: total,
			},
		}
	}
	t.Run("ByOffset", func(t *testing.T) {
		step := 2
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListSeasonTitleEligibility(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.SeasonTitleEligibility), step)
			require.Subset(t, msgs, resp.SeasonTitleEligibility)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListSeasonTitleEligibility(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.SeasonTitleEligibility), step)
			require.Subset(t, msgs, resp.SeasonTitleEligibility)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListSeasonTitleEligibility(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.SeasonTitleEligibility)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListSeasonTitleEligibility(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
