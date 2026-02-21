package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func createNTleDecryptionShare(keeper keeper.Keeper, ctx context.Context, n int) []types.TleDecryptionShare {
	items := make([]types.TleDecryptionShare, n)
	for i := range items {
		items[i].Index = strconv.Itoa(i)
		items[i].SubmittedAt = int64(i)
		_ = keeper.TleDecryptionShare.Set(ctx, items[i].Index, items[i])
	}
	return items
}

func TestTleDecryptionShareQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTleDecryptionShare(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetTleDecryptionShareRequest
		response *types.QueryGetTleDecryptionShareResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetTleDecryptionShareRequest{
				Index: msgs[0].Index,
			},
			response: &types.QueryGetTleDecryptionShareResponse{TleDecryptionShare: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetTleDecryptionShareRequest{
				Index: msgs[1].Index,
			},
			response: &types.QueryGetTleDecryptionShareResponse{TleDecryptionShare: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetTleDecryptionShareRequest{
				Index: strconv.Itoa(100000),
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
			response, err := qs.GetTleDecryptionShare(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestTleDecryptionShareQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTleDecryptionShare(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllTleDecryptionShareRequest {
		return &types.QueryAllTleDecryptionShareRequest{
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
			resp, err := qs.ListTleDecryptionShare(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.TleDecryptionShare), step)
			require.Subset(t, msgs, resp.TleDecryptionShare)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListTleDecryptionShare(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.TleDecryptionShare), step)
			require.Subset(t, msgs, resp.TleDecryptionShare)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListTleDecryptionShare(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.TleDecryptionShare)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListTleDecryptionShare(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
