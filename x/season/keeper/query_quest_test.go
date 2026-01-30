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

func createNQuest(keeper keeper.Keeper, ctx context.Context, n int) []types.Quest {
	items := make([]types.Quest, n)
	for i := range items {
		items[i].QuestId = strconv.Itoa(i)
		items[i].Name = strconv.Itoa(i)
		items[i].Description = strconv.Itoa(i)
		items[i].XpReward = uint64(i)
		items[i].Repeatable = true
		items[i].CooldownEpochs = uint64(i)
		items[i].Season = uint64(i)
		items[i].StartBlock = int64(i)
		items[i].EndBlock = int64(i)
		items[i].Active = true
		items[i].MinLevel = uint64(i)
		items[i].RequiredAchievement = strconv.Itoa(i)
		items[i].PrerequisiteQuest = strconv.Itoa(i)
		items[i].ChainId = strconv.Itoa(i)
		_ = keeper.Quest.Set(ctx, items[i].QuestId, items[i])
	}
	return items
}

func TestQuestQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNQuest(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetQuestRequest
		response *types.QueryGetQuestResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetQuestRequest{
				QuestId: msgs[0].QuestId,
			},
			response: &types.QueryGetQuestResponse{Quest: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetQuestRequest{
				QuestId: msgs[1].QuestId,
			},
			response: &types.QueryGetQuestResponse{Quest: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetQuestRequest{
				QuestId: strconv.Itoa(100000),
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
			response, err := qs.GetQuest(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestQuestQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNQuest(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllQuestRequest {
		return &types.QueryAllQuestRequest{
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
			resp, err := qs.ListQuest(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Quest), step)
			require.Subset(t, msgs, resp.Quest)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListQuest(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Quest), step)
			require.Subset(t, msgs, resp.Quest)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListQuest(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Quest)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListQuest(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
