package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// Helper to create N disputes in the store
func createNDispute(keeper keeper.Keeper, ctx context.Context, n int) []types.Dispute {
	items := make([]types.Dispute, n)
	for i := range items {
		// We use the iteration number to generate unique names
		items[i].Name = strconv.Itoa(i)
		items[i].Claimant = strconv.Itoa(i)

		_ = keeper.Disputes.Set(ctx, items[i].Name, items[i])
	}
	return items
}

func TestDisputeQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNDispute(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetDisputeRequest
		response *types.QueryGetDisputeResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetDisputeRequest{
				Name: msgs[0].Name,
			},
			response: &types.QueryGetDisputeResponse{Dispute: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetDisputeRequest{
				Name: msgs[1].Name,
			},
			response: &types.QueryGetDisputeResponse{Dispute: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetDisputeRequest{
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
			response, err := qs.GetDispute(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.response, response)
			}
		})
	}
}

func TestDisputeQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNDispute(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllDisputeRequest {
		return &types.QueryAllDisputeRequest{
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
			resp, err := qs.ListDispute(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Dispute), step)
			require.Subset(t, msgs, resp.Dispute)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListDispute(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Dispute), step)
			require.Subset(t, msgs, resp.Dispute)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListDispute(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.Equal(t, msgs, resp.Dispute)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListDispute(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
