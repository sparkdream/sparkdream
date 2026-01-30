package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func createNCategory(keeper keeper.Keeper, ctx context.Context, n int) []types.Category {
	items := make([]types.Category, n)
	for i := range items {
		items[i].CategoryId = uint64(i)
		items[i].Title = strconv.Itoa(i)
		items[i].Description = strconv.Itoa(i)
		items[i].MembersOnlyWrite = true
		items[i].AdminOnlyWrite = true
		_ = keeper.Category.Set(ctx, items[i].CategoryId, items[i])
	}
	return items
}

func TestCategoryQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNCategory(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetCategoryRequest
		response *types.QueryGetCategoryResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetCategoryRequest{
				CategoryId: msgs[0].CategoryId,
			},
			response: &types.QueryGetCategoryResponse{Category: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetCategoryRequest{
				CategoryId: msgs[1].CategoryId,
			},
			response: &types.QueryGetCategoryResponse{Category: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetCategoryRequest{
				CategoryId: 100000,
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
			response, err := qs.GetCategory(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestCategoryQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNCategory(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllCategoryRequest {
		return &types.QueryAllCategoryRequest{
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
			resp, err := qs.ListCategory(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Category), step)
			require.Subset(t, msgs, resp.Category)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListCategory(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Category), step)
			require.Subset(t, msgs, resp.Category)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListCategory(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Category)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListCategory(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
