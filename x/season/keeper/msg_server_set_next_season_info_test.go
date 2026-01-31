package keeper_test

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerSetNextSeasonInfo(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SetNextSeasonInfo(f.ctx, &types.MsgSetNextSeasonInfo{
			Authority: "invalid-address",
			Name:      "Season 2",
			Theme:     "Exploration",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not commons council", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		_, err := ms.SetNextSeasonInfo(ctx, &types.MsgSetNextSeasonInfo{
			Authority: nonAuthorityStr,
			Name:      "Season 2",
			Theme:     "Exploration",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotCommonsCouncil)
	})

	t.Run("empty name not allowed", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.SetNextSeasonInfo(ctx, &types.MsgSetNextSeasonInfo{
			Authority: authority,
			Name:      "",
			Theme:     "Some theme",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameTooShort)
	})

	t.Run("name too long", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		longName := strings.Repeat("a", 101) // Max is 100

		_, err := ms.SetNextSeasonInfo(ctx, &types.MsgSetNextSeasonInfo{
			Authority: authority,
			Name:      longName,
			Theme:     "Some theme",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameTooLong)
	})

	t.Run("theme too long", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		longTheme := strings.Repeat("b", 201) // Max is 200

		_, err := ms.SetNextSeasonInfo(ctx, &types.MsgSetNextSeasonInfo{
			Authority: authority,
			Name:      "Season 2",
			Theme:     longTheme,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameTooLong)
	})

	t.Run("successful set next season info", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Need a season to exist for the handler
		SetupDefaultSeason(t, k, ctx)

		_, err := ms.SetNextSeasonInfo(ctx, &types.MsgSetNextSeasonInfo{
			Authority: authority,
			Name:      "Season of Discovery",
			Theme:     "Explore new territories",
		})

		require.NoError(t, err)

		// Verify next season info was set
		info, err := k.NextSeasonInfo.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, "Season of Discovery", info.Name)
		require.Equal(t, "Explore new territories", info.Theme)
	})

	t.Run("update existing next season info", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Need a season to exist for the handler
		SetupDefaultSeason(t, k, ctx)

		// Set initial info
		_, err := ms.SetNextSeasonInfo(ctx, &types.MsgSetNextSeasonInfo{
			Authority: authority,
			Name:      "Initial Name",
			Theme:     "Initial Theme",
		})
		require.NoError(t, err)

		// Update info
		_, err = ms.SetNextSeasonInfo(ctx, &types.MsgSetNextSeasonInfo{
			Authority: authority,
			Name:      "Updated Name",
			Theme:     "Updated Theme",
		})
		require.NoError(t, err)

		// Verify updated values
		info, err := k.NextSeasonInfo.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, "Updated Name", info.Name)
		require.Equal(t, "Updated Theme", info.Theme)
	})

	t.Run("empty theme is allowed", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Need a season to exist for the handler
		SetupDefaultSeason(t, k, ctx)

		_, err := ms.SetNextSeasonInfo(ctx, &types.MsgSetNextSeasonInfo{
			Authority: authority,
			Name:      "Season No Theme",
			Theme:     "",
		})

		require.NoError(t, err)

		info, err := k.NextSeasonInfo.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, "Season No Theme", info.Name)
		require.Equal(t, "", info.Theme)
	})
}
