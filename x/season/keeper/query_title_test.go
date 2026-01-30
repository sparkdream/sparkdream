package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func createNTitle(keeper keeper.Keeper, ctx context.Context, n int) []types.Title {
	items := make([]types.Title, n)
	for i := range items {
		items[i].TitleId = strconv.Itoa(i)
		items[i].Name = strconv.Itoa(i)
		items[i].Description = strconv.Itoa(i)
		items[i].Rarity = uint64(i)
		items[i].RequirementType = uint64(i)
		items[i].RequirementThreshold = uint64(i)
		items[i].RequirementSeason = uint64(i)
		items[i].Seasonal = true
		_ = keeper.Title.Set(ctx, items[i].TitleId, items[i])
	}
	return items
}

func TestTitleQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTitle(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetTitleRequest
		response *types.QueryGetTitleResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetTitleRequest{
				TitleId: msgs[0].TitleId,
			},
			response: &types.QueryGetTitleResponse{Title: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetTitleRequest{
				TitleId: msgs[1].TitleId,
			},
			response: &types.QueryGetTitleResponse{Title: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetTitleRequest{
				TitleId: strconv.Itoa(100000),
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
			response, err := qs.GetTitle(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestTitleQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTitle(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllTitleRequest {
		return &types.QueryAllTitleRequest{
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
			resp, err := qs.ListTitle(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Title), step)
			require.Subset(t, msgs, resp.Title)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListTitle(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Title), step)
			require.Subset(t, msgs, resp.Title)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListTitle(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Title)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListTitle(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
