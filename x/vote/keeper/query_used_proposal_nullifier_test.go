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

func createNUsedProposalNullifier(keeper keeper.Keeper, ctx context.Context, n int) []types.UsedProposalNullifier {
	items := make([]types.UsedProposalNullifier, n)
	for i := range items {
		items[i].Index = strconv.Itoa(i)
		items[i].UsedAt = int64(i)
		_ = keeper.UsedProposalNullifier.Set(ctx, items[i].Index, items[i])
	}
	return items
}

func TestUsedProposalNullifierQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNUsedProposalNullifier(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetUsedProposalNullifierRequest
		response *types.QueryGetUsedProposalNullifierResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetUsedProposalNullifierRequest{
				Index: msgs[0].Index,
			},
			response: &types.QueryGetUsedProposalNullifierResponse{UsedProposalNullifier: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetUsedProposalNullifierRequest{
				Index: msgs[1].Index,
			},
			response: &types.QueryGetUsedProposalNullifierResponse{UsedProposalNullifier: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetUsedProposalNullifierRequest{
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
			response, err := qs.GetUsedProposalNullifier(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestUsedProposalNullifierQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNUsedProposalNullifier(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllUsedProposalNullifierRequest {
		return &types.QueryAllUsedProposalNullifierRequest{
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
			resp, err := qs.ListUsedProposalNullifier(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.UsedProposalNullifier), step)
			require.Subset(t, msgs, resp.UsedProposalNullifier)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListUsedProposalNullifier(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.UsedProposalNullifier), step)
			require.Subset(t, msgs, resp.UsedProposalNullifier)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListUsedProposalNullifier(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.UsedProposalNullifier)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListUsedProposalNullifier(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
