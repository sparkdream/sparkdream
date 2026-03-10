package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func createNGroup(keeper keeper.Keeper, ctx context.Context, n int) []types.Group {
	items := make([]types.Group, n)
	for i := range items {
		maxSpendPerEpoch := math.NewInt(int64(i))
		items[i].Index = strconv.Itoa(i)
		items[i].GroupId = uint64(i)
		items[i].PolicyAddress = strconv.Itoa(i)
		items[i].ParentPolicyAddress = strconv.Itoa(i)
		items[i].FundingWeight = uint64(i)
		items[i].MaxSpendPerEpoch = &maxSpendPerEpoch
		items[i].UpdateCooldown = int64(i)
		items[i].FutarchyEnabled = true
		items[i].MinMembers = uint64(i)
		items[i].MaxMembers = uint64(i)
		items[i].TermDuration = int64(i)
		items[i].CurrentTermExpiration = int64(i)
		items[i].ActivationTime = int64(i)
		_ = keeper.Groups.Set(ctx, items[i].Index, items[i])
	}
	return items
}

func TestGroupQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGroup(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetGroupRequest
		response *types.QueryGetGroupResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetGroupRequest{
				Index: msgs[0].Index,
			},
			response: &types.QueryGetGroupResponse{Group: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetGroupRequest{
				Index: msgs[1].Index,
			},
			response: &types.QueryGetGroupResponse{Group: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetGroupRequest{
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
			response, err := qs.GetGroup(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestGroupQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGroup(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllGroupRequest {
		return &types.QueryAllGroupRequest{
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
			resp, err := qs.ListGroups(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Group), step)
			require.Subset(t, msgs, resp.Group)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListGroups(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Group), step)
			require.Subset(t, msgs, resp.Group)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListGroups(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Group)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListGroups(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
