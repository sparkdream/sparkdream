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

func createNJuryParticipation(keeper keeper.Keeper, ctx context.Context, n int) []types.JuryParticipation {
	items := make([]types.JuryParticipation, n)
	for i := range items {
		items[i].Juror = strconv.Itoa(i)
		items[i].TotalAssigned = uint64(i)
		items[i].TotalVoted = uint64(i)
		items[i].TotalTimeouts = uint64(i)
		items[i].LastAssignedAt = int64(i)
		items[i].Excluded = true
		_ = keeper.JuryParticipation.Set(ctx, items[i].Juror, items[i])
	}
	return items
}

func TestJuryParticipationQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNJuryParticipation(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetJuryParticipationRequest
		response *types.QueryGetJuryParticipationResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetJuryParticipationRequest{
				Juror: msgs[0].Juror,
			},
			response: &types.QueryGetJuryParticipationResponse{JuryParticipation: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetJuryParticipationRequest{
				Juror: msgs[1].Juror,
			},
			response: &types.QueryGetJuryParticipationResponse{JuryParticipation: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetJuryParticipationRequest{
				Juror: strconv.Itoa(100000),
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
			response, err := qs.GetJuryParticipation(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestJuryParticipationQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNJuryParticipation(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllJuryParticipationRequest {
		return &types.QueryAllJuryParticipationRequest{
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
			resp, err := qs.ListJuryParticipation(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.JuryParticipation), step)
			require.Subset(t, msgs, resp.JuryParticipation)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListJuryParticipation(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.JuryParticipation), step)
			require.Subset(t, msgs, resp.JuryParticipation)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListJuryParticipation(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.JuryParticipation)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListJuryParticipation(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
