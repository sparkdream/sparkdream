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

func createNVoterRegistration(keeper keeper.Keeper, ctx context.Context, n int) []types.VoterRegistration {
	items := make([]types.VoterRegistration, n)
	for i := range items {
		items[i].Address = strconv.Itoa(i)
		items[i].RegisteredAt = int64(i)
		items[i].Active = true
		_ = keeper.VoterRegistration.Set(ctx, items[i].Address, items[i])
	}
	return items
}

func TestVoterRegistrationQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVoterRegistration(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetVoterRegistrationRequest
		response *types.QueryGetVoterRegistrationResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetVoterRegistrationRequest{
				Address: msgs[0].Address,
			},
			response: &types.QueryGetVoterRegistrationResponse{VoterRegistration: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetVoterRegistrationRequest{
				Address: msgs[1].Address,
			},
			response: &types.QueryGetVoterRegistrationResponse{VoterRegistration: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetVoterRegistrationRequest{
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
			response, err := qs.GetVoterRegistration(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestVoterRegistrationQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVoterRegistration(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllVoterRegistrationRequest {
		return &types.QueryAllVoterRegistrationRequest{
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
			resp, err := qs.ListVoterRegistration(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VoterRegistration), step)
			require.Subset(t, msgs, resp.VoterRegistration)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListVoterRegistration(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VoterRegistration), step)
			require.Subset(t, msgs, resp.VoterRegistration)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListVoterRegistration(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.VoterRegistration)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListVoterRegistration(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
