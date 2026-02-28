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

func TestQueryListNominationsByCreator(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ListNominationsByCreator(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty creator returns error", func(t *testing.T) {
		_, err := qs.ListNominationsByCreator(f.ctx, &types.QueryListNominationsByCreatorRequest{
			Creator: "",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("filters by creator correctly", func(t *testing.T) {
		// Create nominations from different creators
		nom1 := types.Nomination{
			Id:           10,
			Nominator:    "cosmos1alice",
			ContentRef:   "blog/post/1",
			Rationale:    "Alice nomination 1",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		nom2 := types.Nomination{
			Id:           11,
			Nominator:    "cosmos1alice",
			ContentRef:   "blog/post/2",
			Rationale:    "Alice nomination 2",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}
		nom3 := types.Nomination{
			Id:           12,
			Nominator:    "cosmos1bob",
			ContentRef:   "forum/post/5",
			Rationale:    "Bob nomination",
			Season:       1,
			TotalStaked:  math.LegacyZeroDec(),
			Conviction:   math.LegacyZeroDec(),
			RewardAmount: math.LegacyZeroDec(),
		}

		err := f.keeper.Nomination.Set(f.ctx, nom1.Id, nom1)
		require.NoError(t, err)
		err = f.keeper.Nomination.Set(f.ctx, nom2.Id, nom2)
		require.NoError(t, err)
		err = f.keeper.Nomination.Set(f.ctx, nom3.Id, nom3)
		require.NoError(t, err)

		// Query for Alice's nominations
		resp, err := qs.ListNominationsByCreator(f.ctx, &types.QueryListNominationsByCreatorRequest{
			Creator: "cosmos1alice",
		})
		require.NoError(t, err)
		require.Len(t, resp.Nominations, 2)
		for _, nom := range resp.Nominations {
			require.Equal(t, "cosmos1alice", nom.Nominator)
		}

		// Query for Bob's nominations
		resp, err = qs.ListNominationsByCreator(f.ctx, &types.QueryListNominationsByCreatorRequest{
			Creator: "cosmos1bob",
		})
		require.NoError(t, err)
		require.Len(t, resp.Nominations, 1)
		require.Equal(t, "cosmos1bob", resp.Nominations[0].Nominator)
	})

	t.Run("returns empty when no matches", func(t *testing.T) {
		resp, err := qs.ListNominationsByCreator(f.ctx, &types.QueryListNominationsByCreatorRequest{
			Creator: "cosmos1nonexistent",
		})
		require.NoError(t, err)
		require.Len(t, resp.Nominations, 0)
	})
}
