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

func createNMemberWarning(keeper keeper.Keeper, ctx context.Context, n int) []types.MemberWarning {
	items := make([]types.MemberWarning, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].Member = strconv.Itoa(i)
		items[i].Reason = strconv.Itoa(i)
		items[i].IssuedAt = int64(i)
		items[i].IssuedBy = strconv.Itoa(i)
		items[i].WarningNumber = uint64(i)
		_ = keeper.MemberWarning.Set(ctx, iu, items[i])
		_ = keeper.MemberWarningSeq.Set(ctx, iu)
	}
	return items
}

func TestMemberWarningQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberWarning(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetMemberWarningRequest
		response *types.QueryGetMemberWarningResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetMemberWarningRequest{Id: msgs[0].Id},
			response: &types.QueryGetMemberWarningResponse{MemberWarning: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetMemberWarningRequest{Id: msgs[1].Id},
			response: &types.QueryGetMemberWarningResponse{MemberWarning: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetMemberWarningRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetMemberWarning(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestMemberWarningQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberWarning(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllMemberWarningRequest {
		return &types.QueryAllMemberWarningRequest{
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
			resp, err := qs.ListMemberWarning(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberWarning), step)
			require.Subset(t, msgs, resp.MemberWarning)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListMemberWarning(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberWarning), step)
			require.Subset(t, msgs, resp.MemberWarning)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListMemberWarning(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.MemberWarning)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListMemberWarning(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
