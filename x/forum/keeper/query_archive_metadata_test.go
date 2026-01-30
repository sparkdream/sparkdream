package keeper_test

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func createNArchiveMetadata(keeper keeper.Keeper, ctx context.Context, n int) []types.ArchiveMetadata {
	items := make([]types.ArchiveMetadata, n)
	for i := range items {
		items[i].RootId = uint64(i)
		items[i].ArchiveCount = uint64(i)
		items[i].FirstArchivedAt = int64(i)
		items[i].LastArchivedAt = int64(i)
		items[i].HrOverrideRequired = true
		_ = keeper.ArchiveMetadata.Set(ctx, items[i].RootId, items[i])
	}
	return items
}

func TestArchiveMetadataQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNArchiveMetadata(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetArchiveMetadataRequest
		response *types.QueryGetArchiveMetadataResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetArchiveMetadataRequest{
				RootId: msgs[0].RootId,
			},
			response: &types.QueryGetArchiveMetadataResponse{ArchiveMetadata: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetArchiveMetadataRequest{
				RootId: msgs[1].RootId,
			},
			response: &types.QueryGetArchiveMetadataResponse{ArchiveMetadata: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetArchiveMetadataRequest{
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
			response, err := qs.GetArchiveMetadata(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestArchiveMetadataQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNArchiveMetadata(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllArchiveMetadataRequest {
		return &types.QueryAllArchiveMetadataRequest{
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
			resp, err := qs.ListArchiveMetadata(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ArchiveMetadata), step)
			require.Subset(t, msgs, resp.ArchiveMetadata)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListArchiveMetadata(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ArchiveMetadata), step)
			require.Subset(t, msgs, resp.ArchiveMetadata)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListArchiveMetadata(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ArchiveMetadata)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListArchiveMetadata(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
