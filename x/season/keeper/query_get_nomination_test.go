package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryGetNomination(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.GetNomination(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("nomination not found", func(t *testing.T) {
		_, err := qs.GetNomination(f.ctx, &types.QueryGetNominationRequest{
			Id: 999,
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("successful retrieval", func(t *testing.T) {
		nom := types.Nomination{
			Id:           1,
			Nominator:    "cosmos1creator",
			ContentRef:   "blog/post/42",
			Rationale:    "Excellent work on governance proposal",
			Season:       1,
			TotalStaked:  math.LegacyNewDec(100),
			Conviction:   math.LegacyNewDec(50),
			RewardAmount: math.LegacyZeroDec(),
			Rewarded:     false,
		}
		err := f.keeper.Nomination.Set(f.ctx, nom.Id, nom)
		require.NoError(t, err)

		resp, err := qs.GetNomination(f.ctx, &types.QueryGetNominationRequest{
			Id: 1,
		})
		require.NoError(t, err)
		require.Equal(t, uint64(1), resp.Nomination.Id)
		require.Equal(t, "cosmos1creator", resp.Nomination.Nominator)
		require.Equal(t, "blog/post/42", resp.Nomination.ContentRef)
		require.Equal(t, "Excellent work on governance proposal", resp.Nomination.Rationale)
		require.Equal(t, uint64(1), resp.Nomination.Season)
		require.True(t, resp.Nomination.TotalStaked.Equal(math.LegacyNewDec(100)))
		require.True(t, resp.Nomination.Conviction.Equal(math.LegacyNewDec(50)))
		require.True(t, resp.Nomination.RewardAmount.IsZero())
		require.False(t, resp.Nomination.Rewarded)
	})
}
