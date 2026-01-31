package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func createNDisplayNameReportStake(keeper keeper.Keeper, ctx context.Context, n int) []types.DisplayNameReportStake {
	items := make([]types.DisplayNameReportStake, n)
	for i := range items {
		items[i].ChallengeId = strconv.Itoa(i)
		items[i].Reporter = strconv.Itoa(i)
		items[i].Amount = math.NewInt(int64(i))
		_ = keeper.DisplayNameReportStake.Set(ctx, items[i].ChallengeId, items[i])
	}
	return items
}

func TestDisplayNameReportStakeQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNDisplayNameReportStake(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetDisplayNameReportStakeRequest
		response *types.QueryGetDisplayNameReportStakeResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetDisplayNameReportStakeRequest{
				ChallengeId: msgs[0].ChallengeId,
			},
			response: &types.QueryGetDisplayNameReportStakeResponse{DisplayNameReportStake: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetDisplayNameReportStakeRequest{
				ChallengeId: msgs[1].ChallengeId,
			},
			response: &types.QueryGetDisplayNameReportStakeResponse{DisplayNameReportStake: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetDisplayNameReportStakeRequest{
				ChallengeId: strconv.Itoa(100000),
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
			response, err := qs.GetDisplayNameReportStake(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestDisplayNameReportStakeQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNDisplayNameReportStake(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllDisplayNameReportStakeRequest {
		return &types.QueryAllDisplayNameReportStakeRequest{
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
			resp, err := qs.ListDisplayNameReportStake(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.DisplayNameReportStake), step)
			require.Subset(t, msgs, resp.DisplayNameReportStake)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListDisplayNameReportStake(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.DisplayNameReportStake), step)
			require.Subset(t, msgs, resp.DisplayNameReportStake)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListDisplayNameReportStake(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.DisplayNameReportStake)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListDisplayNameReportStake(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
