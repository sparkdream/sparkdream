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

func createNTag(keeper keeper.Keeper, ctx context.Context, n int) []commontypes.Tag {
	items := make([]commontypes.Tag, n)
	for i := range items {
		items[i].Name = strconv.Itoa(i)
		items[i].UsageCount = uint64(i)
		items[i].CreatedAt = int64(i)
		items[i].LastUsedAt = int64(i)
		items[i].ExpirationIndex = int64(i)
		_ = keeper.Tag.Set(ctx, items[i].Name, items[i])
	}
	return items
}

func TestTagQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTag(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetTagRequest
		response *types.QueryGetTagResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetTagRequest{
				Name: msgs[0].Name,
			},
			response: &types.QueryGetTagResponse{Tag: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetTagRequest{
				Name: msgs[1].Name,
			},
			response: &types.QueryGetTagResponse{Tag: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetTagRequest{
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
			response, err := qs.GetTag(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestTagQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTag(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllTagRequest {
		return &types.QueryAllTagRequest{
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
			resp, err := qs.ListTag(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Tag), step)
			require.Subset(t, msgs, resp.Tag)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListTag(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Tag), step)
			require.Subset(t, msgs, resp.Tag)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListTag(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Tag)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListTag(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
