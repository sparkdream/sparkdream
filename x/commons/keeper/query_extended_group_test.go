package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func createNExtendedGroup(keeper keeper.Keeper, ctx context.Context, n int) []types.ExtendedGroup {
	items := make([]types.ExtendedGroup, n)
	for i := range items {
		items[i].Index = strconv.Itoa(i)
		items[i].GroupId = uint64(i)
		items[i].PolicyAddress = strconv.Itoa(i)
		items[i].ParentPolicyAddress = strconv.Itoa(i)
		items[i].FundingWeight = uint64(i)
		items[i].MaxSpendPerEpoch = strconv.Itoa(i)
		items[i].UpdateCooldown = int64(i)
		items[i].FutarchyEnabled = true
		items[i].MinMembers = uint64(i)
		items[i].MaxMembers = uint64(i)
		items[i].TermDuration = int64(i)
		items[i].CurrentTermExpiration = int64(i)
		items[i].ActivationTime = int64(i)
		_ = keeper.ExtendedGroup.Set(ctx, items[i].Index, items[i])
	}
	return items
}

func TestExtendedGroupQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNExtendedGroup(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetExtendedGroupRequest
		response *types.QueryGetExtendedGroupResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetExtendedGroupRequest{
				Index: msgs[0].Index,
			},
			response: &types.QueryGetExtendedGroupResponse{ExtendedGroup: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetExtendedGroupRequest{
				Index: msgs[1].Index,
			},
			response: &types.QueryGetExtendedGroupResponse{ExtendedGroup: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetExtendedGroupRequest{
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
			response, err := qs.GetExtendedGroup(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestExtendedGroupQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNExtendedGroup(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllExtendedGroupRequest {
		return &types.QueryAllExtendedGroupRequest{
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
			resp, err := qs.ListExtendedGroup(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ExtendedGroup), step)
			require.Subset(t, msgs, resp.ExtendedGroup)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListExtendedGroup(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ExtendedGroup), step)
			require.Subset(t, msgs, resp.ExtendedGroup)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListExtendedGroup(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ExtendedGroup)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListExtendedGroup(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
