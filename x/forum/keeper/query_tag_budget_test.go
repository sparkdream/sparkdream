package keeper_test

import (
	"context"
	"strconv"
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func createNTagBudget(keeper keeper.Keeper, ctx context.Context, n int) []types.TagBudget {
	items := make([]types.TagBudget, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].GroupAccount = strconv.Itoa(i)
		items[i].Tag = strconv.Itoa(i)
		items[i].PoolBalance = strconv.Itoa(i)
		items[i].MembersOnly = true
		items[i].CreatedAt = int64(i)
		items[i].Active = true
		_ = keeper.TagBudget.Set(ctx, iu, items[i])
		_ = keeper.TagBudgetSeq.Set(ctx, iu)
	}
	return items
}

func TestTagBudgetQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTagBudget(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetTagBudgetRequest
		response *types.QueryGetTagBudgetResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetTagBudgetRequest{Id: msgs[0].Id},
			response: &types.QueryGetTagBudgetResponse{TagBudget: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetTagBudgetRequest{Id: msgs[1].Id},
			response: &types.QueryGetTagBudgetResponse{TagBudget: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetTagBudgetRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetTagBudget(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestTagBudgetQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTagBudget(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllTagBudgetRequest {
		return &types.QueryAllTagBudgetRequest{
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
			resp, err := qs.ListTagBudget(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.TagBudget), step)
			require.Subset(t, msgs, resp.TagBudget)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListTagBudget(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.TagBudget), step)
			require.Subset(t, msgs, resp.TagBudget)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListTagBudget(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.TagBudget)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListTagBudget(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
