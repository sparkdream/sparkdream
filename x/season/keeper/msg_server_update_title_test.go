package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerUpdateTitle(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.UpdateTitle(f.ctx, &types.MsgUpdateTitle{
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

		_, err := ms.UpdateTitle(ctx, &types.MsgUpdateTitle{
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

		_, err := ms.UpdateTitle(ctx, &types.MsgUpdateTitle{
			Authority: authority,
			TitleId:   "nonexistent",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTitleNotFound)
	})

	t.Run("successful update via governance", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create title first
		existing := types.Title{
			TitleId:              "title1",
			Name:                 "Original Name",
			Description:          "Original Description",
			Rarity:               types.Rarity_RARITY_COMMON,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_VOTES_CAST,
			RequirementThreshold: 5,
			RequirementSeason:    0,
			Seasonal:             false,
		}
		k.Title.Set(ctx, "title1", existing)

		// Update it
		_, err := ms.UpdateTitle(ctx, &types.MsgUpdateTitle{
			Authority:            authority,
			TitleId:              "title1",
			Name:                 "Updated Name",
			Description:          "Updated Description",
			Rarity:               uint32(types.Rarity_RARITY_EPIC),
			RequirementType:      uint32(types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE),
			RequirementThreshold: 10,
			RequirementSeason:    2,
			Seasonal:             true,
		})

		require.NoError(t, err)

		// Verify updates
		title, err := k.Title.Get(ctx, "title1")
		require.NoError(t, err)
		require.Equal(t, "Updated Name", title.Name)
		require.Equal(t, "Updated Description", title.Description)
		require.Equal(t, types.Rarity_RARITY_EPIC, title.Rarity)
		require.Equal(t, types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE, title.RequirementType)
		require.Equal(t, uint64(10), title.RequirementThreshold)
		require.Equal(t, uint64(2), title.RequirementSeason)
		require.True(t, title.Seasonal)
	})

	t.Run("successful update via operations committee", func(t *testing.T) {
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
			Name:    "Original",
		}
		k.Title.Set(ctx, "title1", existing)

		// Update it
		_, err := ms.UpdateTitle(ctx, &types.MsgUpdateTitle{
			Authority: addrStr,
			TitleId:   "title1",
			Name:      "Committee Updated",
		})

		require.NoError(t, err)

		// Verify
		title, err := k.Title.Get(ctx, "title1")
		require.NoError(t, err)
		require.Equal(t, "Committee Updated", title.Name)
	})

	t.Run("toggle seasonal flag", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create non-seasonal title
		existing := types.Title{
			TitleId:  "title1",
			Name:     "Permanent Title",
			Seasonal: false,
		}
		k.Title.Set(ctx, "title1", existing)

		// Make it seasonal
		_, err := ms.UpdateTitle(ctx, &types.MsgUpdateTitle{
			Authority: authority,
			TitleId:   "title1",
			Name:      "Now Seasonal",
			Seasonal:  true,
		})

		require.NoError(t, err)

		title, err := k.Title.Get(ctx, "title1")
		require.NoError(t, err)
		require.True(t, title.Seasonal)

		// Make it permanent again
		_, err = ms.UpdateTitle(ctx, &types.MsgUpdateTitle{
			Authority: authority,
			TitleId:   "title1",
			Name:      "Permanent Again",
			Seasonal:  false,
		})

		require.NoError(t, err)

		title, err = k.Title.Get(ctx, "title1")
		require.NoError(t, err)
		require.False(t, title.Seasonal)
	})
}
