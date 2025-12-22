package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/split/keeper"
	"sparkdream/x/split/types"
)

func createNShare(keeper keeper.Keeper, ctx context.Context, n int) []types.Share {
	items := make([]types.Share, n)
	for i := range items {
		items[i].Address = strconv.Itoa(i)
		items[i].Weight = uint64(i)
		_ = keeper.Share.Set(ctx, items[i].Address, items[i])
	}
	return items
}

func TestShareQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNShare(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetShareRequest
		response *types.QueryGetShareResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetShareRequest{
				Address: msgs[0].Address,
			},
			response: &types.QueryGetShareResponse{Share: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetShareRequest{
				Address: msgs[1].Address,
			},
			response: &types.QueryGetShareResponse{Share: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetShareRequest{
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
			response, err := qs.GetShare(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestShareQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNShare(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllShareRequest {
		return &types.QueryAllShareRequest{
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
			resp, err := qs.ListShare(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Share), step)
			require.Subset(t, msgs, resp.Share)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListShare(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Share), step)
			require.Subset(t, msgs, resp.Share)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListShare(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Share)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListShare(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
