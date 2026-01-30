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

func createNUserReactionLimit(keeper keeper.Keeper, ctx context.Context, n int) []types.UserReactionLimit {
	items := make([]types.UserReactionLimit, n)
	for i := range items {
		items[i].UserAddress = strconv.Itoa(i)
		items[i].CurrentDayCount = uint64(i)
		items[i].PreviousDayCount = uint64(i)
		items[i].CurrentDayStart = int64(i)
		_ = keeper.UserReactionLimit.Set(ctx, items[i].UserAddress, items[i])
	}
	return items
}

func TestUserReactionLimitQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNUserReactionLimit(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetUserReactionLimitRequest
		response *types.QueryGetUserReactionLimitResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetUserReactionLimitRequest{
				UserAddress: msgs[0].UserAddress,
			},
			response: &types.QueryGetUserReactionLimitResponse{UserReactionLimit: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetUserReactionLimitRequest{
				UserAddress: msgs[1].UserAddress,
			},
			response: &types.QueryGetUserReactionLimitResponse{UserReactionLimit: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetUserReactionLimitRequest{
				UserAddress: strconv.Itoa(100000),
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
			response, err := qs.GetUserReactionLimit(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestUserReactionLimitQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNUserReactionLimit(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllUserReactionLimitRequest {
		return &types.QueryAllUserReactionLimitRequest{
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
			resp, err := qs.ListUserReactionLimit(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.UserReactionLimit), step)
			require.Subset(t, msgs, resp.UserReactionLimit)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListUserReactionLimit(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.UserReactionLimit), step)
			require.Subset(t, msgs, resp.UserReactionLimit)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListUserReactionLimit(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.UserReactionLimit)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListUserReactionLimit(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
