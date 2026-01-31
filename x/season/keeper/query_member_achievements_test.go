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

func TestQueryMemberAchievements(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberAchievements(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty member address", func(t *testing.T) {
		_, err := qs.MemberAchievements(ctx, &types.QueryMemberAchievementsRequest{Address: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("member not found", func(t *testing.T) {
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		resp, err := qs.MemberAchievements(ctx, &types.QueryMemberAchievementsRequest{Address: memberStr})
		require.NoError(t, err)
		require.Empty(t, resp.AchievementId)
	})

	t.Run("member with no achievements", func(t *testing.T) {
		member := TestAddrMember2
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, member)

		resp, err := qs.MemberAchievements(ctx, &types.QueryMemberAchievementsRequest{Address: memberStr})
		require.NoError(t, err)
		require.Empty(t, resp.AchievementId)
	})

	t.Run("member with achievements", func(t *testing.T) {
		member := TestAddrMember3
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, member)

		// Add achievements to profile
		profile, _ := k.MemberProfile.Get(ctx, memberStr)
		profile.Achievements = []string{"first_blood", "veteran"}
		k.MemberProfile.Set(ctx, memberStr, profile)

		resp, err := qs.MemberAchievements(ctx, &types.QueryMemberAchievementsRequest{Address: memberStr})
		require.NoError(t, err)
		require.Equal(t, "first_blood", resp.AchievementId)
	})
}
