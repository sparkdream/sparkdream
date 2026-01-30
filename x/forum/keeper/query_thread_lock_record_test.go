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

func createNThreadLockRecord(keeper keeper.Keeper, ctx context.Context, n int) []types.ThreadLockRecord {
	items := make([]types.ThreadLockRecord, n)
	for i := range items {
		items[i].RootId = uint64(i)
		items[i].Sentinel = strconv.Itoa(i)
		items[i].LockedAt = int64(i)
		items[i].SentinelBondSnapshot = strconv.Itoa(i)
		items[i].SentinelBackingSnapshot = strconv.Itoa(i)
		items[i].LockReason = strconv.Itoa(i)
		items[i].AppealPending = true
		items[i].InitiativeId = uint64(i)
		_ = keeper.ThreadLockRecord.Set(ctx, items[i].RootId, items[i])
	}
	return items
}

func TestThreadLockRecordQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadLockRecord(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetThreadLockRecordRequest
		response *types.QueryGetThreadLockRecordResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetThreadLockRecordRequest{
				RootId: msgs[0].RootId,
			},
			response: &types.QueryGetThreadLockRecordResponse{ThreadLockRecord: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetThreadLockRecordRequest{
				RootId: msgs[1].RootId,
			},
			response: &types.QueryGetThreadLockRecordResponse{ThreadLockRecord: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetThreadLockRecordRequest{
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
			response, err := qs.GetThreadLockRecord(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestThreadLockRecordQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadLockRecord(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllThreadLockRecordRequest {
		return &types.QueryAllThreadLockRecordRequest{
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
			resp, err := qs.ListThreadLockRecord(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadLockRecord), step)
			require.Subset(t, msgs, resp.ThreadLockRecord)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListThreadLockRecord(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadLockRecord), step)
			require.Subset(t, msgs, resp.ThreadLockRecord)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListThreadLockRecord(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ThreadLockRecord)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListThreadLockRecord(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
