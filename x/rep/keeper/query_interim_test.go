package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createNInterim(keeper keeper.Keeper, ctx context.Context, n int) []types.Interim {
	items := make([]types.Interim, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].Type = types.InterimType(i)
		items[i].Assignees = []string{strconv.Itoa(i)}
		items[i].Committee = strconv.Itoa(i)
		items[i].ReferenceId = uint64(i)
		items[i].ReferenceType = strconv.Itoa(i)
		items[i].Complexity = types.InterimComplexity(i)
		amount := math.NewInt(int64(i))
		items[i].Budget = &amount
		items[i].Deadline = int64(i)
		items[i].CreatedAt = int64(i)
		items[i].CompletedAt = int64(i)
		items[i].Status = types.InterimStatus(i)
		items[i].CompletionNotes = strconv.Itoa(i)
		_ = keeper.Interim.Set(ctx, iu, items[i])
		_ = keeper.InterimSeq.Set(ctx, iu)
	}
	return items
}

func TestInterimQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNInterim(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetInterimRequest
		response *types.QueryGetInterimResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetInterimRequest{Id: msgs[0].Id},
			response: &types.QueryGetInterimResponse{Interim: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetInterimRequest{Id: msgs[1].Id},
			response: &types.QueryGetInterimResponse{Interim: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetInterimRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetInterim(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestInterimQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNInterim(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllInterimRequest {
		return &types.QueryAllInterimRequest{
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
			resp, err := qs.ListInterim(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Interim), step)
			require.Subset(t, msgs, resp.Interim)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListInterim(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Interim), step)
			require.Subset(t, msgs, resp.Interim)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListInterim(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Interim)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListInterim(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
