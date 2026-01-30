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

func createNThreadMetadata(keeper keeper.Keeper, ctx context.Context, n int) []types.ThreadMetadata {
	items := make([]types.ThreadMetadata, n)
	for i := range items {
		items[i].ThreadId = uint64(i)
		items[i].AcceptedReplyId = uint64(i)
		items[i].AcceptedBy = strconv.Itoa(i)
		items[i].AcceptedAt = int64(i)
		items[i].ProposedReplyId = uint64(i)
		items[i].ProposedBy = strconv.Itoa(i)
		items[i].ProposedAt = int64(i)
		_ = keeper.ThreadMetadata.Set(ctx, items[i].ThreadId, items[i])
	}
	return items
}

func TestThreadMetadataQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadMetadata(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetThreadMetadataRequest
		response *types.QueryGetThreadMetadataResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetThreadMetadataRequest{
				ThreadId: msgs[0].ThreadId,
			},
			response: &types.QueryGetThreadMetadataResponse{ThreadMetadata: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetThreadMetadataRequest{
				ThreadId: msgs[1].ThreadId,
			},
			response: &types.QueryGetThreadMetadataResponse{ThreadMetadata: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetThreadMetadataRequest{
				ThreadId: 100000,
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
			response, err := qs.GetThreadMetadata(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestThreadMetadataQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadMetadata(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllThreadMetadataRequest {
		return &types.QueryAllThreadMetadataRequest{
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
			resp, err := qs.ListThreadMetadata(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadMetadata), step)
			require.Subset(t, msgs, resp.ThreadMetadata)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListThreadMetadata(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadMetadata), step)
			require.Subset(t, msgs, resp.ThreadMetadata)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListThreadMetadata(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ThreadMetadata)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListThreadMetadata(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
