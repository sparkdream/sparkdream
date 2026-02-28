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

func TestQueryListNominations(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ListNominations(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty result", func(t *testing.T) {
		resp, err := qs.ListNominations(f.ctx, &types.QueryListNominationsRequest{})
		require.NoError(t, err)
		require.Len(t, resp.Nominations, 0)
	})

	t.Run("returns all nominations", func(t *testing.T) {
		nom1 := types.Nomination{
			Id:           1,
			Nominator:    "cosmos1alice",
			ContentRef:   "blog/post/10",
			Rationale:    "First nomination",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
			Rewarded:     false,
		}
		nom2 := types.Nomination{
			Id:           2,
			Nominator:    "cosmos1bob",
			ContentRef:   "forum/post/20",
			Rationale:    "Second nomination",
			Season:       1,
			TotalStaked:  math.LegacyNewDec(50),
			Conviction:   math.LegacyNewDec(25),
			RewardAmount: math.LegacyZeroDec(),
			Rewarded:     false,
		}
		nom3 := types.Nomination{
			Id:           3,
			Nominator:    "cosmos1carol",
			ContentRef:   "collect/collection/5",
			Rationale:    "Third nomination",
			Season:       2,
			TotalStaked:  math.LegacyNewDec(200),
			Conviction:   math.LegacyNewDec(100),
			RewardAmount: math.LegacyNewDec(500),
			Rewarded:     true,
		}

		err := f.keeper.Nomination.Set(f.ctx, nom1.Id, nom1)
		require.NoError(t, err)
		err = f.keeper.Nomination.Set(f.ctx, nom2.Id, nom2)
		require.NoError(t, err)
		err = f.keeper.Nomination.Set(f.ctx, nom3.Id, nom3)
		require.NoError(t, err)

		resp, err := qs.ListNominations(f.ctx, &types.QueryListNominationsRequest{})
		require.NoError(t, err)
		require.Len(t, resp.Nominations, 3)

		// Verify IDs are present (order guaranteed by uint64 key)
		require.Equal(t, uint64(1), resp.Nominations[0].Id)
		require.Equal(t, uint64(2), resp.Nominations[1].Id)
		require.Equal(t, uint64(3), resp.Nominations[2].Id)
	})
}
