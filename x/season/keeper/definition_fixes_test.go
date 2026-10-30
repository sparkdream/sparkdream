package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// An achievement a quest requires cannot be deleted: the quest would become
// unstartable for everyone who has not earned it.
func TestDeleteAchievementRequiredByQuestRefused(t *testing.T) {
	f := initFixtureWithCommons(t, newMockCommonsKeeperWithCouncil(TestAddrCouncilPolicy.String()))
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	authority, _ := f.addressCodec.BytesToString(TestAddrCouncilPolicy)

	require.NoError(t, k.SetAchievement(ctx, types.Achievement{AchievementId: "phoenix_badge", Name: "Phoenix"}))
	require.NoError(t, k.Quest.Set(ctx, "aurora_quest", types.Quest{QuestId: "aurora_quest", Name: "Aurora", RequiredAchievement: "phoenix_badge", Active: true}))

	_, err := ms.DeleteAchievement(ctx, &types.MsgDeleteAchievement{Authority: authority, AchievementId: "phoenix_badge"})
	require.ErrorIs(t, err, types.ErrInvalidAchievementId)

	require.NoError(t, k.Quest.Remove(ctx, "aurora_quest"))
	_, err = ms.DeleteAchievement(ctx, &types.MsgDeleteAchievement{Authority: authority, AchievementId: "phoenix_badge"})
	require.NoError(t, err)
}

// Achievement and title definitions must use defined enum values and stay
// within the name/description limits.
func TestDefinitionValidation(t *testing.T) {
	f := initFixtureWithCommons(t, newMockCommonsKeeperWithCouncil(TestAddrCouncilPolicy.String()))
	ctx := sdk.UnwrapSDKContext(f.ctx)
	ms := keeper.NewMsgServerImpl(f.keeper)
	authority, _ := f.addressCodec.BytesToString(TestAddrCouncilPolicy)

	_, err := ms.CreateAchievement(ctx, &types.MsgCreateAchievement{Authority: authority, AchievementId: "a1", Name: "Zenith", RequirementType: 999})
	require.ErrorIs(t, err, types.ErrInvalidAchievementId, "undefined requirement type")
	_, err = ms.CreateTitle(ctx, &types.MsgCreateTitle{Authority: authority, TitleId: "t1", Name: "Zenith", Rarity: 77})
	require.ErrorIs(t, err, types.ErrInvalidTitleId, "undefined rarity")
	long := make([]byte, types.MaxDefinitionNameLength+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err = ms.CreateTitle(ctx, &types.MsgCreateTitle{Authority: authority, TitleId: "t2", Name: string(long)})
	require.ErrorIs(t, err, types.ErrInvalidTitleId, "name too long")
	_, err = ms.CreateTitle(ctx, &types.MsgCreateTitle{Authority: authority, TitleId: "t3", Name: "Zenith", Rarity: 3, RequirementType: 7})
	require.NoError(t, err)
}

// A deleted title stops occupying a profile's slots: it is dropped when the
// profile next gains a title, and the titles query skips it.
func TestDeletedTitleDroppedFromProfiles(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	member, _ := f.addressCodec.BytesToString(TestAddrMember1)
	SetupBasicMemberProfile(t, k, ctx, TestAddrMember1)
	require.NoError(t, k.SetTitle(ctx, types.Title{TitleId: "gone", Name: "Gone"}))
	require.NoError(t, k.SetTitle(ctx, types.Title{TitleId: "kept", Name: "Kept"}))
	require.NoError(t, k.UnlockTitle(ctx, member, "gone"))
	profile, err := k.MemberProfile.Get(ctx, member)
	require.NoError(t, err)
	profile.DisplayTitle = "gone"
	require.NoError(t, k.MemberProfile.Set(ctx, member, profile))

	require.NoError(t, k.RemoveTitle(ctx, "gone"))
	resp, err := keeper.NewQueryServerImpl(k).MemberTitles(ctx, &types.QueryMemberTitlesRequest{Address: member})
	require.NoError(t, err)
	require.Empty(t, resp.TitleId, "the deleted title is not reported")

	require.NoError(t, k.UnlockTitle(ctx, member, "kept"))
	profile, err = k.MemberProfile.Get(ctx, member)
	require.NoError(t, err)
	require.Equal(t, []string{"kept"}, profile.UnlockedTitles)
	require.Empty(t, profile.DisplayTitle)
}
