package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerDeleteTitle(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.DeleteTitle(f.ctx, &types.MsgDeleteTitle{
			Authority: "invalid-address",
			TitleId:   "title1",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not authorized", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		_, err := ms.DeleteTitle(ctx, &types.MsgDeleteTitle{
			Authority: nonAuthorityStr,
			TitleId:   "title1",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotAuthorized)
	})

	t.Run("title not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.DeleteTitle(ctx, &types.MsgDeleteTitle{
			Authority: authority,
			TitleId:   "nonexistent",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTitleNotFound)
	})

	t.Run("successful deletion via governance", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create title first
		existing := types.Title{
			TitleId: "title1",
			Name:    "To Be Deleted",
		}
		k.Title.Set(ctx, "title1", existing)

		// Verify it exists
		_, err := k.Title.Get(ctx, "title1")
		require.NoError(t, err)

		// Delete it
		_, err = ms.DeleteTitle(ctx, &types.MsgDeleteTitle{
			Authority: authority,
			TitleId:   "title1",
		})

		require.NoError(t, err)

		// Verify it's gone
		_, err = k.Title.Get(ctx, "title1")
		require.Error(t, err)
	})

	t.Run("successful deletion via operations committee", func(t *testing.T) {
		committeeAddr := TestAddrMember1
		committeeAddrStr := committeeAddr.String()

		mockCommons := newMockCommonsKeeper(committeeAddrStr)
		f := initFixtureWithCommons(t, mockCommons)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		addrStr, _ := f.addressCodec.BytesToString(committeeAddr)

		// Create title first
		existing := types.Title{
			TitleId: "title1",
			Name:    "Committee Delete",
		}
		k.Title.Set(ctx, "title1", existing)

		// Delete it
		_, err := ms.DeleteTitle(ctx, &types.MsgDeleteTitle{
			Authority: addrStr,
			TitleId:   "title1",
		})

		require.NoError(t, err)

		// Verify it's gone
		_, err = k.Title.Get(ctx, "title1")
		require.Error(t, err)
	})

	t.Run("successful deletion via commons council policy", func(t *testing.T) {
		councilPolicyAddr := TestAddrCouncilPolicy
		mockCommons := newMockCommonsKeeperWithCouncil(councilPolicyAddr.String())
		f := initFixtureWithCommons(t, mockCommons)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		addrStr, _ := f.addressCodec.BytesToString(councilPolicyAddr)

		// Create title first
		existing := types.Title{
			TitleId: "council_title",
			Name:    "Council Title",
		}
		k.Title.Set(ctx, "council_title", existing)

		// Delete via council policy
		_, err := ms.DeleteTitle(ctx, &types.MsgDeleteTitle{
			Authority: addrStr,
			TitleId:   "council_title",
		})

		require.NoError(t, err)

		// Verify it's gone
		_, err = k.Title.Get(ctx, "council_title")
		require.Error(t, err)
	})
}
