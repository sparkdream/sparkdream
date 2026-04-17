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

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createNTagBudgetAward(k keeper.Keeper, ctx context.Context, n int) []types.TagBudgetAward {
	items := make([]types.TagBudgetAward, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].BudgetId = uint64(i)
		items[i].PostId = uint64(i)
		items[i].Recipient = strconv.Itoa(i)
		items[i].Amount = strconv.Itoa(i)
		items[i].Reason = strconv.Itoa(i)
		items[i].AwardedAt = int64(i)
		items[i].AwardedBy = strconv.Itoa(i)
		_ = k.TagBudgetAward.Set(ctx, iu, items[i])
		_ = k.TagBudgetAwardSeq.Set(ctx, iu)
	}
	return items
}

func TestTagBudgetAwardQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTagBudgetAward(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetTagBudgetAwardRequest
		response *types.QueryGetTagBudgetAwardResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetTagBudgetAwardRequest{Id: msgs[0].Id},
			response: &types.QueryGetTagBudgetAwardResponse{TagBudgetAward: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetTagBudgetAwardRequest{Id: msgs[1].Id},
			response: &types.QueryGetTagBudgetAwardResponse{TagBudgetAward: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetTagBudgetAwardRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetTagBudgetAward(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestTagBudgetAwardQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTagBudgetAward(f.keeper, f.ctx, 5)

	req := func(next []byte, offset, limit uint64, total bool) *types.QueryAllTagBudgetAwardRequest {
		return &types.QueryAllTagBudgetAwardRequest{
			Pagination: &query.PageRequest{Key: next, Offset: offset, Limit: limit, CountTotal: total},
		}
	}
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListTagBudgetAward(f.ctx, req(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.TagBudgetAward)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListTagBudgetAward(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
