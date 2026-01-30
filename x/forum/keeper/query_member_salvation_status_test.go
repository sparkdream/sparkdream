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

func createNMemberSalvationStatus(keeper keeper.Keeper, ctx context.Context, n int) []types.MemberSalvationStatus {
	items := make([]types.MemberSalvationStatus, n)
	for i := range items {
		items[i].Address = strconv.Itoa(i)
		items[i].MemberSince = int64(i)
		items[i].CanSalvage = true
		items[i].EpochSalvations = uint64(i)
		items[i].EpochStart = int64(i)
		_ = keeper.MemberSalvationStatus.Set(ctx, items[i].Address, items[i])
	}
	return items
}

func TestMemberSalvationStatusQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberSalvationStatus(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetMemberSalvationStatusRequest
		response *types.QueryGetMemberSalvationStatusResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetMemberSalvationStatusRequest{
				Address: msgs[0].Address,
			},
			response: &types.QueryGetMemberSalvationStatusResponse{MemberSalvationStatus: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetMemberSalvationStatusRequest{
				Address: msgs[1].Address,
			},
			response: &types.QueryGetMemberSalvationStatusResponse{MemberSalvationStatus: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetMemberSalvationStatusRequest{
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
			response, err := qs.GetMemberSalvationStatus(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestMemberSalvationStatusQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberSalvationStatus(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllMemberSalvationStatusRequest {
		return &types.QueryAllMemberSalvationStatusRequest{
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
			resp, err := qs.ListMemberSalvationStatus(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberSalvationStatus), step)
			require.Subset(t, msgs, resp.MemberSalvationStatus)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListMemberSalvationStatus(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberSalvationStatus), step)
			require.Subset(t, msgs, resp.MemberSalvationStatus)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListMemberSalvationStatus(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.MemberSalvationStatus)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListMemberSalvationStatus(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
