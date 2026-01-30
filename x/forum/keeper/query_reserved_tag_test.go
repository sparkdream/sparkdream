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

func createNReservedTag(keeper keeper.Keeper, ctx context.Context, n int) []types.ReservedTag {
	items := make([]types.ReservedTag, n)
	for i := range items {
		items[i].Name = strconv.Itoa(i)
		items[i].Authority = strconv.Itoa(i)
		items[i].MembersCanUse = true
		_ = keeper.ReservedTag.Set(ctx, items[i].Name, items[i])
	}
	return items
}

func TestReservedTagQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNReservedTag(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetReservedTagRequest
		response *types.QueryGetReservedTagResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetReservedTagRequest{
				Name: msgs[0].Name,
			},
			response: &types.QueryGetReservedTagResponse{ReservedTag: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetReservedTagRequest{
				Name: msgs[1].Name,
			},
			response: &types.QueryGetReservedTagResponse{ReservedTag: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetReservedTagRequest{
				Name: strconv.Itoa(100000),
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
			response, err := qs.GetReservedTag(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestReservedTagQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNReservedTag(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllReservedTagRequest {
		return &types.QueryAllReservedTagRequest{
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
			resp, err := qs.ListReservedTag(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ReservedTag), step)
			require.Subset(t, msgs, resp.ReservedTag)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListReservedTag(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ReservedTag), step)
			require.Subset(t, msgs, resp.ReservedTag)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListReservedTag(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ReservedTag)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListReservedTag(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
