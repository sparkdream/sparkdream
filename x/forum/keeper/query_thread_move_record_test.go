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

func createNThreadMoveRecord(keeper keeper.Keeper, ctx context.Context, n int) []types.ThreadMoveRecord {
	items := make([]types.ThreadMoveRecord, n)
	for i := range items {
		items[i].RootId = uint64(i)
		items[i].Sentinel = strconv.Itoa(i)
		items[i].OriginalCategoryId = uint64(i)
		items[i].NewCategoryId = uint64(i)
		items[i].MovedAt = int64(i)
		items[i].SentinelBondSnapshot = strconv.Itoa(i)
		items[i].SentinelBackingSnapshot = strconv.Itoa(i)
		items[i].MoveReason = strconv.Itoa(i)
		items[i].AppealPending = true
		items[i].InitiativeId = uint64(i)
		_ = keeper.ThreadMoveRecord.Set(ctx, items[i].RootId, items[i])
	}
	return items
}

func TestThreadMoveRecordQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadMoveRecord(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetThreadMoveRecordRequest
		response *types.QueryGetThreadMoveRecordResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetThreadMoveRecordRequest{
				RootId: msgs[0].RootId,
			},
			response: &types.QueryGetThreadMoveRecordResponse{ThreadMoveRecord: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetThreadMoveRecordRequest{
				RootId: msgs[1].RootId,
			},
			response: &types.QueryGetThreadMoveRecordResponse{ThreadMoveRecord: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetThreadMoveRecordRequest{
				RootId: 100000,
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
			response, err := qs.GetThreadMoveRecord(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestThreadMoveRecordQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadMoveRecord(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllThreadMoveRecordRequest {
		return &types.QueryAllThreadMoveRecordRequest{
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
			resp, err := qs.ListThreadMoveRecord(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadMoveRecord), step)
			require.Subset(t, msgs, resp.ThreadMoveRecord)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListThreadMoveRecord(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadMoveRecord), step)
			require.Subset(t, msgs, resp.ThreadMoveRecord)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListThreadMoveRecord(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ThreadMoveRecord)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListThreadMoveRecord(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
