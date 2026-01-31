package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerSetDisplayTitle(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SetDisplayTitle(f.ctx, &types.MsgSetDisplayTitle{
			Creator: "invalid-address",
			TitleId: "title1",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("profile not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// No profile exists

		_, err := ms.SetDisplayTitle(ctx, &types.MsgSetDisplayTitle{
			Creator: creatorStr,
			TitleId: "title1",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrProfileNotFound)
	})

	t.Run("title not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		_, err := ms.SetDisplayTitle(ctx, &types.MsgSetDisplayTitle{
			Creator: creatorStr,
			TitleId: "nonexistent_title",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTitleNotFound)
	})

	t.Run("title not unlocked", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create a title but don't unlock it for the user
		title := types.Title{
			TitleId:     "locked_title",
			Name:        "Locked Title",
			Description: "A title that is locked",
		}
		k.Title.Set(ctx, "locked_title", title)

		_, err := ms.SetDisplayTitle(ctx, &types.MsgSetDisplayTitle{
			Creator: creatorStr,
			TitleId: "locked_title",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTitleNotUnlocked)
	})

	t.Run("successful set display title", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create a title
		title := types.Title{
			TitleId:     "test_title",
			Name:        "Test Champion",
			Description: "A title for testing",
		}
		k.Title.Set(ctx, "test_title", title)

		// Unlock the title for the user by adding to profile.UnlockedTitles
		profile, _ := k.MemberProfile.Get(ctx, creatorStr)
		profile.UnlockedTitles = []string{"test_title"}
		k.MemberProfile.Set(ctx, creatorStr, profile)

		_, err := ms.SetDisplayTitle(ctx, &types.MsgSetDisplayTitle{
			Creator: creatorStr,
			TitleId: "test_title",
		})

		require.NoError(t, err)

		// Verify title was set
		profile, _ = k.MemberProfile.Get(ctx, creatorStr)
		require.Equal(t, "test_title", profile.DisplayTitle)
	})

	t.Run("clear display title with empty string", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember2
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Set a title first
		profile, _ := k.MemberProfile.Get(ctx, creatorStr)
		profile.DisplayTitle = "some_title"
		k.MemberProfile.Set(ctx, creatorStr, profile)

		// Clear the title
		_, err := ms.SetDisplayTitle(ctx, &types.MsgSetDisplayTitle{
			Creator: creatorStr,
			TitleId: "", // Empty to clear
		})

		require.NoError(t, err)

		// Verify title was cleared
		profile, _ = k.MemberProfile.Get(ctx, creatorStr)
		require.Equal(t, "", profile.DisplayTitle)
	})

	t.Run("change to different title", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember3
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create two titles
		title1 := types.Title{TitleId: "title_a", Name: "Title A"}
		title2 := types.Title{TitleId: "title_b", Name: "Title B"}
		k.Title.Set(ctx, "title_a", title1)
		k.Title.Set(ctx, "title_b", title2)

		// Unlock both titles via profile
		profile, _ := k.MemberProfile.Get(ctx, creatorStr)
		profile.UnlockedTitles = []string{"title_a", "title_b"}
		k.MemberProfile.Set(ctx, creatorStr, profile)

		// Set first title
		_, err := ms.SetDisplayTitle(ctx, &types.MsgSetDisplayTitle{
			Creator: creatorStr,
			TitleId: "title_a",
		})
		require.NoError(t, err)

		profile, _ = k.MemberProfile.Get(ctx, creatorStr)
		require.Equal(t, "title_a", profile.DisplayTitle)

		// Change to second title
		_, err = ms.SetDisplayTitle(ctx, &types.MsgSetDisplayTitle{
			Creator: creatorStr,
			TitleId: "title_b",
		})
		require.NoError(t, err)

		profile, _ = k.MemberProfile.Get(ctx, creatorStr)
		require.Equal(t, "title_b", profile.DisplayTitle)
	})
}
