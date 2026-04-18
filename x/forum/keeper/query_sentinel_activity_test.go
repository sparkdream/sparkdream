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

func createNSentinelActivity(keeper keeper.Keeper, ctx context.Context, n int) []types.SentinelActivity {
	items := make([]types.SentinelActivity, n)
	for i := range items {
		items[i].Address = strconv.Itoa(i)
		items[i].TotalHides = uint64(i)
		items[i].UpheldHides = uint64(i)
		items[i].OverturnedHides = uint64(i)
		items[i].UnchallengedHides = uint64(i)
		items[i].EpochHides = uint64(i)
		items[i].EpochAppealsResolved = uint64(i)
		items[i].OverturnCooldownUntil = int64(i)
		items[i].ConsecutiveOverturns = uint64(i)
		items[i].PendingHideCount = uint64(i)
		items[i].ConsecutiveUpheld = uint64(i)
		items[i].EpochAppealsFiled = uint64(i)
		items[i].TotalLocks = uint64(i)
		items[i].UpheldLocks = uint64(i)
		items[i].OverturnedLocks = uint64(i)
		items[i].EpochLocks = uint64(i)
		items[i].TotalMoves = uint64(i)
		items[i].UpheldMoves = uint64(i)
		items[i].OverturnedMoves = uint64(i)
		items[i].EpochMoves = uint64(i)
		items[i].TotalPins = uint64(i)
		items[i].UpheldPins = uint64(i)
		items[i].OverturnedPins = uint64(i)
		items[i].EpochPins = uint64(i)
		items[i].TotalProposals = uint64(i)
		items[i].ConfirmedProposals = uint64(i)
		items[i].RejectedProposals = uint64(i)
		items[i].EpochCurations = uint64(i)
		_ = keeper.SentinelActivity.Set(ctx, items[i].Address, items[i])
	}
	return items
}

func TestSentinelActivityQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNSentinelActivity(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetSentinelActivityRequest
		response *types.QueryGetSentinelActivityResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetSentinelActivityRequest{
				Address: msgs[0].Address,
			},
			response: &types.QueryGetSentinelActivityResponse{SentinelActivity: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetSentinelActivityRequest{
				Address: msgs[1].Address,
			},
			response: &types.QueryGetSentinelActivityResponse{SentinelActivity: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetSentinelActivityRequest{
				Address: strconv.Itoa(100000),
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
			response, err := qs.GetSentinelActivity(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestSentinelActivityQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNSentinelActivity(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllSentinelActivityRequest {
		return &types.QueryAllSentinelActivityRequest{
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
			resp, err := qs.ListSentinelActivity(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.SentinelActivity), step)
			require.Subset(t, msgs, resp.SentinelActivity)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListSentinelActivity(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.SentinelActivity), step)
			require.Subset(t, msgs, resp.SentinelActivity)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListSentinelActivity(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.SentinelActivity)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListSentinelActivity(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
