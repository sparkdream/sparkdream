package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryJuryParticipation(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	participations := []types.JuryParticipation{
		{Juror: "juror-phoenix", TotalAssigned: 5, TotalVoted: 4, TotalTimeouts: 1},
		{Juror: "juror-aurora", TotalAssigned: 10, TotalVoted: 10, TotalTimeouts: 0},
		{Juror: "juror-zenith", TotalAssigned: 2, TotalVoted: 1, TotalTimeouts: 1, Excluded: true},
	}
	for _, p := range participations {
		require.NoError(t, f.keeper.JuryParticipation.Set(f.ctx, p.Juror, p))
	}

	t.Run("get found", func(t *testing.T) {
		resp, err := qs.GetJuryParticipation(f.ctx, &types.QueryGetJuryParticipationRequest{Juror: "juror-aurora"})
		require.NoError(t, err)
		require.Equal(t, "juror-aurora", resp.JuryParticipation.Juror)
		require.Equal(t, uint64(10), resp.JuryParticipation.TotalVoted)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := qs.GetJuryParticipation(f.ctx, &types.QueryGetJuryParticipationRequest{Juror: "nobody"})
		require.Error(t, err)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("get nil request", func(t *testing.T) {
		_, err := qs.GetJuryParticipation(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("list all", func(t *testing.T) {
		resp, err := qs.ListJuryParticipation(f.ctx, &types.QueryAllJuryParticipationRequest{
			Pagination: &query.PageRequest{CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.JuryParticipation, 3)
		require.Equal(t, uint64(3), resp.Pagination.Total)
	})

	t.Run("list paginated", func(t *testing.T) {
		resp, err := qs.ListJuryParticipation(f.ctx, &types.QueryAllJuryParticipationRequest{
			Pagination: &query.PageRequest{Limit: 1, CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.JuryParticipation, 1)
		require.NotEmpty(t, resp.Pagination.NextKey)
	})

	t.Run("list nil request", func(t *testing.T) {
		_, err := qs.ListJuryParticipation(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}
