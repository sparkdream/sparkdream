package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createNTagReport(keeper keeper.Keeper, ctx context.Context, n int) []types.TagReport {
	items := make([]types.TagReport, n)
	for i := range items {
		items[i].TagName = strconv.Itoa(i)
		items[i].TotalBond = strconv.Itoa(i)
		items[i].FirstReportAt = int64(i)
		items[i].UnderReview = true
		_ = keeper.TagReport.Set(ctx, items[i].TagName, items[i])
	}
	return items
}

func TestTagReportQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTagReport(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetTagReportRequest
		response *types.QueryGetTagReportResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetTagReportRequest{TagName: msgs[0].TagName},
			response: &types.QueryGetTagReportResponse{TagReport: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetTagReportRequest{TagName: msgs[1].TagName},
			response: &types.QueryGetTagReportResponse{TagReport: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetTagReportRequest{TagName: strconv.Itoa(100000)},
			err:     status.Error(codes.NotFound, "not found"),
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetTagReport(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestTagReportQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTagReport(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllTagReportRequest {
		return &types.QueryAllTagReportRequest{
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
			resp, err := qs.ListTagReport(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.TagReport), step)
			require.Subset(t, msgs, resp.TagReport)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListTagReport(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.TagReport), step)
			require.Subset(t, msgs, resp.TagReport)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListTagReport(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.TagReport)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListTagReport(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
