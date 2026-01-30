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

func createNEpochXpTracker(keeper keeper.Keeper, ctx context.Context, n int) []types.EpochXpTracker {
	items := make([]types.EpochXpTracker, n)
	for i := range items {
		items[i].MemberEpoch = strconv.Itoa(i)
		items[i].VoteXpEarned = uint64(i)
		items[i].ForumXpEarned = uint64(i)
		items[i].QuestXpEarned = uint64(i)
		items[i].OtherXpEarned = uint64(i)
		_ = keeper.EpochXpTracker.Set(ctx, items[i].MemberEpoch, items[i])
	}
	return items
}

func TestEpochXpTrackerQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNEpochXpTracker(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetEpochXpTrackerRequest
		response *types.QueryGetEpochXpTrackerResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetEpochXpTrackerRequest{
				MemberEpoch: msgs[0].MemberEpoch,
			},
			response: &types.QueryGetEpochXpTrackerResponse{EpochXpTracker: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetEpochXpTrackerRequest{
				MemberEpoch: msgs[1].MemberEpoch,
			},
			response: &types.QueryGetEpochXpTrackerResponse{EpochXpTracker: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetEpochXpTrackerRequest{
				MemberEpoch: strconv.Itoa(100000),
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
			response, err := qs.GetEpochXpTracker(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestEpochXpTrackerQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNEpochXpTracker(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllEpochXpTrackerRequest {
		return &types.QueryAllEpochXpTrackerRequest{
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
			resp, err := qs.ListEpochXpTracker(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.EpochXpTracker), step)
			require.Subset(t, msgs, resp.EpochXpTracker)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListEpochXpTracker(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.EpochXpTracker), step)
			require.Subset(t, msgs, resp.EpochXpTracker)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListEpochXpTracker(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.EpochXpTracker)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListEpochXpTracker(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
