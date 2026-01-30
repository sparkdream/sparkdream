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

func createNMemberQuestProgress(keeper keeper.Keeper, ctx context.Context, n int) []types.MemberQuestProgress {
	items := make([]types.MemberQuestProgress, n)
	for i := range items {
		items[i].MemberQuest = strconv.Itoa(i)
		items[i].Completed = true
		items[i].CompletedBlock = int64(i)
		items[i].LastAttemptBlock = int64(i)
		_ = keeper.MemberQuestProgress.Set(ctx, items[i].MemberQuest, items[i])
	}
	return items
}

func TestMemberQuestProgressQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberQuestProgress(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetMemberQuestProgressRequest
		response *types.QueryGetMemberQuestProgressResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetMemberQuestProgressRequest{
				MemberQuest: msgs[0].MemberQuest,
			},
			response: &types.QueryGetMemberQuestProgressResponse{MemberQuestProgress: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetMemberQuestProgressRequest{
				MemberQuest: msgs[1].MemberQuest,
			},
			response: &types.QueryGetMemberQuestProgressResponse{MemberQuestProgress: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetMemberQuestProgressRequest{
				MemberQuest: strconv.Itoa(100000),
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
			response, err := qs.GetMemberQuestProgress(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestMemberQuestProgressQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberQuestProgress(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllMemberQuestProgressRequest {
		return &types.QueryAllMemberQuestProgressRequest{
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
			resp, err := qs.ListMemberQuestProgress(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberQuestProgress), step)
			require.Subset(t, msgs, resp.MemberQuestProgress)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListMemberQuestProgress(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberQuestProgress), step)
			require.Subset(t, msgs, resp.MemberQuestProgress)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListMemberQuestProgress(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.MemberQuestProgress)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListMemberQuestProgress(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
