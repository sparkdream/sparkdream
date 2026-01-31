package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryMemberSeasonHistory(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberSeasonHistory(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty member address", func(t *testing.T) {
		_, err := qs.MemberSeasonHistory(ctx, &types.QueryMemberSeasonHistoryRequest{Address: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("member with no history", func(t *testing.T) {
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		resp, err := qs.MemberSeasonHistory(ctx, &types.QueryMemberSeasonHistoryRequest{Address: memberStr})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.Season)
	})

	t.Run("member with season history", func(t *testing.T) {
		member := TestAddrMember2
		memberStr, _ := f.addressCodec.BytesToString(member)

		// Create member season snapshot
		// Key format is "seasonNumber/address"
		snapshot := types.MemberSeasonSnapshot{
			SeasonAddress:        "2/" + memberStr,
			XpEarned:             1500,
			SeasonLevel:          15,
			InitiativesCompleted: 10,
			AchievementsEarned:   []string{"first_quest"},
		}
		key := "2/" + memberStr
		k.MemberSeasonSnapshot.Set(ctx, key, snapshot)

		resp, err := qs.MemberSeasonHistory(ctx, &types.QueryMemberSeasonHistoryRequest{Address: memberStr})
		require.NoError(t, err)
		require.Equal(t, uint64(2), resp.Season)
		require.Equal(t, uint64(1500), resp.XpEarned)
		require.Equal(t, uint64(15), resp.Level)
	})
}
