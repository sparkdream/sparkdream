package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerCreateTitle(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CreateTitle(f.ctx, &types.MsgCreateTitle{
			Authority: "invalid-address",
			TitleId:   "title1",
			Name:      "Test Title",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not authorized - no commons keeper", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		_, err := ms.CreateTitle(ctx, &types.MsgCreateTitle{
			Authority: nonAuthorityStr,
			TitleId:   "title1",
			Name:      "Test Title",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotAuthorized)
	})

	t.Run("authorized via operations committee", func(t *testing.T) {
		committeeAddr := TestAddrMember1
		committeeAddrStr := committeeAddr.String()

		mockCommons := newMockCommonsKeeper(committeeAddrStr)
		f := initFixtureWithCommons(t, mockCommons)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		addrStr, _ := f.addressCodec.BytesToString(committeeAddr)

		_, err := ms.CreateTitle(ctx, &types.MsgCreateTitle{
			Authority:            addrStr,
			TitleId:              "title1",
			Name:                 "Test Title",
			Description:          "A test title",
			Rarity:               uint32(types.Rarity_RARITY_RARE),
			RequirementType:      uint32(types.RequirementType_REQUIREMENT_TYPE_TOP_XP),
			RequirementThreshold: 1,
			RequirementSeason:    0,
			Seasonal:             true,
		})

		require.NoError(t, err)

		// Verify title was created
		title, err := f.keeper.Title.Get(ctx, "title1")
		require.NoError(t, err)
		require.Equal(t, "title1", title.TitleId)
		require.Equal(t, "Test Title", title.Name)
		require.Equal(t, "A test title", title.Description)
		require.Equal(t, types.Rarity_RARITY_RARE, title.Rarity)
		require.True(t, title.Seasonal)
	})

	t.Run("authorized via commons council policy", func(t *testing.T) {
		councilPolicyAddr := TestAddrCouncilPolicy
		mockCommons := newMockCommonsKeeperWithCouncil(councilPolicyAddr.String())
		f := initFixtureWithCommons(t, mockCommons)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		addrStr, _ := f.addressCodec.BytesToString(councilPolicyAddr)

		_, err := ms.CreateTitle(ctx, &types.MsgCreateTitle{
			Authority: addrStr,
			TitleId:   "council_title",
			Name:      "Council Title",
		})

		require.NoError(t, err)

		title, err := f.keeper.Title.Get(ctx, "council_title")
		require.NoError(t, err)
		require.Equal(t, "Council Title", title.Name)
	})

	t.Run("authorized via governance authority", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.CreateTitle(ctx, &types.MsgCreateTitle{
			Authority: authority,
			TitleId:   "gov_title",
			Name:      "Governance Title",
		})

		require.NoError(t, err)
	})

	t.Run("empty title id", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.CreateTitle(ctx, &types.MsgCreateTitle{
			Authority: authority,
			TitleId:   "",
			Name:      "Test",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidTitleId)
	})

	t.Run("empty name", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.CreateTitle(ctx, &types.MsgCreateTitle{
			Authority: authority,
			TitleId:   "title1",
			Name:      "",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "title name cannot be empty")
	})

	t.Run("title already exists", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create existing title
		existing := types.Title{
			TitleId: "existing",
			Name:    "Existing",
		}
		k.Title.Set(ctx, "existing", existing)

		_, err := ms.CreateTitle(ctx, &types.MsgCreateTitle{
			Authority: authority,
			TitleId:   "existing",
			Name:      "New Name",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTitleExists)
	})

	t.Run("create title with all options", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.CreateTitle(ctx, &types.MsgCreateTitle{
			Authority:            authority,
			TitleId:              "champion",
			Name:                 "Season Champion",
			Description:          "Awarded to the top player of a season",
			Rarity:               uint32(types.Rarity_RARITY_LEGENDARY),
			RequirementType:      uint32(types.RequirementType_REQUIREMENT_TYPE_TOP_XP),
			RequirementThreshold: 1, // Top 1 player
			RequirementSeason:    5, // Specific season
			Seasonal:             true,
		})

		require.NoError(t, err)

		title, err := k.Title.Get(ctx, "champion")
		require.NoError(t, err)
		require.Equal(t, "Season Champion", title.Name)
		require.Equal(t, types.Rarity_RARITY_LEGENDARY, title.Rarity)
		require.Equal(t, uint64(5), title.RequirementSeason)
		require.True(t, title.Seasonal)
	})
}
