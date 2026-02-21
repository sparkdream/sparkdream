package keeper_test

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func createNEpochDecryptionKey(keeper keeper.Keeper, ctx context.Context, n int) []types.EpochDecryptionKey {
	items := make([]types.EpochDecryptionKey, n)
	for i := range items {
		items[i].Epoch = uint64(i)
		items[i].AvailableAt = int64(i)
		_ = keeper.EpochDecryptionKey.Set(ctx, items[i].Epoch, items[i])
	}
	return items
}

func TestEpochDecryptionKeyQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNEpochDecryptionKey(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetEpochDecryptionKeyRequest
		response *types.QueryGetEpochDecryptionKeyResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetEpochDecryptionKeyRequest{
				Epoch: msgs[0].Epoch,
			},
			response: &types.QueryGetEpochDecryptionKeyResponse{EpochDecryptionKey: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetEpochDecryptionKeyRequest{
				Epoch: msgs[1].Epoch,
			},
			response: &types.QueryGetEpochDecryptionKeyResponse{EpochDecryptionKey: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetEpochDecryptionKeyRequest{
				Epoch: 100000,
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
			response, err := qs.GetEpochDecryptionKey(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestEpochDecryptionKeyQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNEpochDecryptionKey(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllEpochDecryptionKeyRequest {
		return &types.QueryAllEpochDecryptionKeyRequest{
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
			resp, err := qs.ListEpochDecryptionKey(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.EpochDecryptionKey), step)
			require.Subset(t, msgs, resp.EpochDecryptionKey)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListEpochDecryptionKey(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.EpochDecryptionKey), step)
			require.Subset(t, msgs, resp.EpochDecryptionKey)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListEpochDecryptionKey(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.EpochDecryptionKey)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListEpochDecryptionKey(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
