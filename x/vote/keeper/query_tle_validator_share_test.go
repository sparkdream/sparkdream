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

func createNTleValidatorShare(keeper keeper.Keeper, ctx context.Context, n int) []types.TleValidatorShare {
	items := make([]types.TleValidatorShare, n)
	for i := range items {
		items[i].Validator = strconv.Itoa(i)
		items[i].ShareIndex = uint64(i)
		items[i].RegisteredAt = int64(i)
		_ = keeper.TleValidatorShare.Set(ctx, items[i].Validator, items[i])
	}
	return items
}

func TestTleValidatorShareQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTleValidatorShare(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetTleValidatorShareRequest
		response *types.QueryGetTleValidatorShareResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetTleValidatorShareRequest{
				Validator: msgs[0].Validator,
			},
			response: &types.QueryGetTleValidatorShareResponse{TleValidatorShare: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetTleValidatorShareRequest{
				Validator: msgs[1].Validator,
			},
			response: &types.QueryGetTleValidatorShareResponse{TleValidatorShare: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetTleValidatorShareRequest{
				Validator: strconv.Itoa(100000),
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
			response, err := qs.GetTleValidatorShare(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestTleValidatorShareQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNTleValidatorShare(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllTleValidatorShareRequest {
		return &types.QueryAllTleValidatorShareRequest{
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
			resp, err := qs.ListTleValidatorShare(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.TleValidatorShare), step)
			require.Subset(t, msgs, resp.TleValidatorShare)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListTleValidatorShare(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.TleValidatorShare), step)
			require.Subset(t, msgs, resp.TleValidatorShare)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListTleValidatorShare(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.TleValidatorShare)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListTleValidatorShare(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
