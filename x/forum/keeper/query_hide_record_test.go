package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func createNHideRecord(keeper keeper.Keeper, ctx context.Context, n int) []types.HideRecord {
	items := make([]types.HideRecord, n)
	for i := range items {
		items[i].PostId = uint64(i)
		items[i].Sentinel = strconv.Itoa(i)
		items[i].HiddenAt = int64(i)
		items[i].SentinelBondSnapshot = strconv.Itoa(i)
		items[i].SentinelBackingSnapshot = strconv.Itoa(i)
		items[i].CommittedAmount = strconv.Itoa(i)
		items[i].ReasonCode = commontypes.ModerationReason(i)
		items[i].ReasonText = strconv.Itoa(i)
		_ = keeper.HideRecord.Set(ctx, items[i].PostId, items[i])
	}
	return items
}

func TestHideRecordQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNHideRecord(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetHideRecordRequest
		response *types.QueryGetHideRecordResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetHideRecordRequest{
				PostId: msgs[0].PostId,
			},
			response: &types.QueryGetHideRecordResponse{HideRecord: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetHideRecordRequest{
				PostId: msgs[1].PostId,
			},
			response: &types.QueryGetHideRecordResponse{HideRecord: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetHideRecordRequest{
				PostId: 100000,
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
			response, err := qs.GetHideRecord(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestHideRecordQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNHideRecord(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllHideRecordRequest {
		return &types.QueryAllHideRecordRequest{
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
			resp, err := qs.ListHideRecord(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.HideRecord), step)
			require.Subset(t, msgs, resp.HideRecord)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListHideRecord(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.HideRecord), step)
			require.Subset(t, msgs, resp.HideRecord)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListHideRecord(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.HideRecord)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListHideRecord(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
