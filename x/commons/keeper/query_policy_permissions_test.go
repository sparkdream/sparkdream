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

func createNPolicyPermissions(keeper keeper.Keeper, ctx context.Context, n int) []types.PolicyPermissions {
	items := make([]types.PolicyPermissions, n)
	for i := range items {
		items[i].PolicyAddress = strconv.Itoa(i)
		items[i].AllowedMessages = []string{`abc` + strconv.Itoa(i), `xyz` + strconv.Itoa(i)}
		_ = keeper.PolicyPermissions.Set(ctx, items[i].PolicyAddress, items[i])
	}
	return items
}

func TestPolicyPermissionsQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNPolicyPermissions(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetPolicyPermissionsRequest
		response *types.QueryGetPolicyPermissionsResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetPolicyPermissionsRequest{
				PolicyAddress: msgs[0].PolicyAddress,
			},
			response: &types.QueryGetPolicyPermissionsResponse{PolicyPermissions: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetPolicyPermissionsRequest{
				PolicyAddress: msgs[1].PolicyAddress,
			},
			response: &types.QueryGetPolicyPermissionsResponse{PolicyPermissions: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetPolicyPermissionsRequest{
				PolicyAddress: strconv.Itoa(100000),
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
			response, err := qs.GetPolicyPermissions(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestPolicyPermissionsQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNPolicyPermissions(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllPolicyPermissionsRequest {
		return &types.QueryAllPolicyPermissionsRequest{
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
			resp, err := qs.ListPolicyPermissions(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.PolicyPermissions), step)
			require.Subset(t, msgs, resp.PolicyPermissions)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListPolicyPermissions(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.PolicyPermissions), step)
			require.Subset(t, msgs, resp.PolicyPermissions)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListPolicyPermissions(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.PolicyPermissions)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListPolicyPermissions(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
