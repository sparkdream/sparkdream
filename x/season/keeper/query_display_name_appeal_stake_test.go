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

func createNDisplayNameAppealStake(keeper keeper.Keeper, ctx context.Context, n int) []types.DisplayNameAppealStake {
	items := make([]types.DisplayNameAppealStake, n)
	for i := range items {
		items[i].ChallengeId = strconv.Itoa(i)
		items[i].Appellant = strconv.Itoa(i)
		items[i].Amount = math.NewInt(int64(i))
		_ = keeper.DisplayNameAppealStake.Set(ctx, items[i].ChallengeId, items[i])
	}
	return items
}

func TestDisplayNameAppealStakeQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNDisplayNameAppealStake(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetDisplayNameAppealStakeRequest
		response *types.QueryGetDisplayNameAppealStakeResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetDisplayNameAppealStakeRequest{
				ChallengeId: msgs[0].ChallengeId,
			},
			response: &types.QueryGetDisplayNameAppealStakeResponse{DisplayNameAppealStake: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetDisplayNameAppealStakeRequest{
				ChallengeId: msgs[1].ChallengeId,
			},
			response: &types.QueryGetDisplayNameAppealStakeResponse{DisplayNameAppealStake: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetDisplayNameAppealStakeRequest{
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
			response, err := qs.GetDisplayNameAppealStake(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestDisplayNameAppealStakeQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNDisplayNameAppealStake(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllDisplayNameAppealStakeRequest {
		return &types.QueryAllDisplayNameAppealStakeRequest{
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
			resp, err := qs.ListDisplayNameAppealStake(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.DisplayNameAppealStake), step)
			require.Subset(t, msgs, resp.DisplayNameAppealStake)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListDisplayNameAppealStake(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.DisplayNameAppealStake), step)
			require.Subset(t, msgs, resp.DisplayNameAppealStake)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListDisplayNameAppealStake(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.DisplayNameAppealStake)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListDisplayNameAppealStake(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
