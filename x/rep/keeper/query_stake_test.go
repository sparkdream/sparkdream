package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createNStake(keeper keeper.Keeper, ctx context.Context, n int) []types.Stake {
	items := make([]types.Stake, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].Staker = strconv.Itoa(i)
		items[i].TargetType = types.StakeTargetType(i)
		items[i].TargetId = uint64(i)
		amount := math.NewInt(int64(i))
		items[i].Amount = &amount
		items[i].CreatedAt = int64(i)
		_ = keeper.Stake.Set(ctx, iu, items[i])
		_ = keeper.StakeSeq.Set(ctx, iu)
	}
	return items
}

func TestStakeQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNStake(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetStakeRequest
		response *types.QueryGetStakeResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetStakeRequest{Id: msgs[0].Id},
			response: &types.QueryGetStakeResponse{Stake: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetStakeRequest{Id: msgs[1].Id},
			response: &types.QueryGetStakeResponse{Stake: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetStakeRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetStake(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestStakeQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNStake(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllStakeRequest {
		return &types.QueryAllStakeRequest{
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
			resp, err := qs.ListStake(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Stake), step)
			require.Subset(t, msgs, resp.Stake)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListStake(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Stake), step)
			require.Subset(t, msgs, resp.Stake)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListStake(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Stake)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListStake(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
