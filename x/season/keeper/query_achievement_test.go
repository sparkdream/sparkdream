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

func createNAchievement(keeper keeper.Keeper, ctx context.Context, n int) []types.Achievement {
	items := make([]types.Achievement, n)
	for i := range items {
		items[i].AchievementId = strconv.Itoa(i)
		items[i].Name = strconv.Itoa(i)
		items[i].Description = strconv.Itoa(i)
		items[i].Rarity = uint64(i)
		items[i].XpReward = uint64(i)
		items[i].RequirementType = uint64(i)
		items[i].RequirementThreshold = uint64(i)
		_ = keeper.Achievement.Set(ctx, items[i].AchievementId, items[i])
	}
	return items
}

func TestAchievementQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNAchievement(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetAchievementRequest
		response *types.QueryGetAchievementResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetAchievementRequest{
				AchievementId: msgs[0].AchievementId,
			},
			response: &types.QueryGetAchievementResponse{Achievement: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetAchievementRequest{
				AchievementId: msgs[1].AchievementId,
			},
			response: &types.QueryGetAchievementResponse{Achievement: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetAchievementRequest{
				AchievementId: strconv.Itoa(100000),
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
			response, err := qs.GetAchievement(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestAchievementQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNAchievement(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllAchievementRequest {
		return &types.QueryAllAchievementRequest{
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
			resp, err := qs.ListAchievement(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Achievement), step)
			require.Subset(t, msgs, resp.Achievement)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListAchievement(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Achievement), step)
			require.Subset(t, msgs, resp.Achievement)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListAchievement(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Achievement)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListAchievement(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
