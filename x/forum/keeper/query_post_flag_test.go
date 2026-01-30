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

func createNPostFlag(keeper keeper.Keeper, ctx context.Context, n int) []types.PostFlag {
	items := make([]types.PostFlag, n)
	for i := range items {
		items[i].PostId = uint64(i)
		items[i].TotalWeight = strconv.Itoa(i)
		items[i].FirstFlagAt = int64(i)
		items[i].LastFlagAt = int64(i)
		items[i].InReviewQueue = true
		_ = keeper.PostFlag.Set(ctx, items[i].PostId, items[i])
	}
	return items
}

func TestPostFlagQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNPostFlag(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetPostFlagRequest
		response *types.QueryGetPostFlagResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetPostFlagRequest{
				PostId: msgs[0].PostId,
			},
			response: &types.QueryGetPostFlagResponse{PostFlag: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetPostFlagRequest{
				PostId: msgs[1].PostId,
			},
			response: &types.QueryGetPostFlagResponse{PostFlag: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetPostFlagRequest{
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
			response, err := qs.GetPostFlag(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestPostFlagQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNPostFlag(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllPostFlagRequest {
		return &types.QueryAllPostFlagRequest{
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
			resp, err := qs.ListPostFlag(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.PostFlag), step)
			require.Subset(t, msgs, resp.PostFlag)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListPostFlag(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.PostFlag), step)
			require.Subset(t, msgs, resp.PostFlag)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListPostFlag(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.PostFlag)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListPostFlag(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
