package keeper_test

import (
	"context"
	"strconv"
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func createNBounty(keeper keeper.Keeper, ctx context.Context, n int) []types.Bounty {
	items := make([]types.Bounty, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].Creator = strconv.Itoa(i)
		items[i].ThreadId = uint64(i)
		items[i].Amount = strconv.Itoa(i)
		items[i].CreatedAt = int64(i)
		items[i].ExpiresAt = int64(i)
		items[i].Status = types.BountyStatus(i)
		items[i].ModerationSuspendedAt = int64(i)
		items[i].TimeRemainingAtSuspension = int64(i)
		_ = keeper.Bounty.Set(ctx, iu, items[i])
		_ = keeper.BountySeq.Set(ctx, iu)
	}
	return items
}

func TestBountyQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNBounty(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetBountyRequest
		response *types.QueryGetBountyResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetBountyRequest{Id: msgs[0].Id},
			response: &types.QueryGetBountyResponse{Bounty: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetBountyRequest{Id: msgs[1].Id},
			response: &types.QueryGetBountyResponse{Bounty: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetBountyRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetBounty(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestBountyQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNBounty(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllBountyRequest {
		return &types.QueryAllBountyRequest{
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
			resp, err := qs.ListBounty(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Bounty), step)
			require.Subset(t, msgs, resp.Bounty)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListBounty(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Bounty), step)
			require.Subset(t, msgs, resp.Bounty)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListBounty(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Bounty)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListBounty(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
