package keeper_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	module "sparkdream/x/season/module"
	"sparkdream/x/season/types"
)

// initFixtureWithCommons creates a test fixture with a mock CommonsKeeper
func initFixtureWithCommons(t *testing.T, commonsKeeper types.CommonsKeeper) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		nil, // bankKeeper
		nil, // repKeeper
		nil, // nameKeeper
		commonsKeeper,
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
	}
}

func TestMsgServerCreateAchievement(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CreateAchievement(f.ctx, &types.MsgCreateAchievement{
			Authority:     "invalid-address",
			AchievementId: "ach1",
			Name:          "Test Achievement",
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

		_, err := ms.CreateAchievement(ctx, &types.MsgCreateAchievement{
			Authority:     nonAuthorityStr,
			AchievementId: "ach1",
			Name:          "Test Achievement",
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

		_, err := ms.CreateAchievement(ctx, &types.MsgCreateAchievement{
			Authority:            addrStr,
			AchievementId:        "ach1",
			Name:                 "Test Achievement",
			Description:          "A test achievement",
			Rarity:               uint32(types.Rarity_RARITY_COMMON),
			XpReward:             100,
			RequirementType:      uint32(types.RequirementType_REQUIREMENT_TYPE_VOTES_CAST),
			RequirementThreshold: 10,
		})

		require.NoError(t, err)

		// Verify achievement was created
		ach, err := f.keeper.Achievement.Get(ctx, "ach1")
		require.NoError(t, err)
		require.Equal(t, "ach1", ach.AchievementId)
		require.Equal(t, "Test Achievement", ach.Name)
		require.Equal(t, "A test achievement", ach.Description)
		require.Equal(t, types.Rarity_RARITY_COMMON, ach.Rarity)
		require.Equal(t, uint64(100), ach.XpReward)
	})

	t.Run("authorized via commons council policy", func(t *testing.T) {
		councilPolicyAddr := TestAddrCouncilPolicy
		mockCommons := newMockCommonsKeeperWithCouncil(councilPolicyAddr.String())
		f := initFixtureWithCommons(t, mockCommons)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		addrStr, _ := f.addressCodec.BytesToString(councilPolicyAddr)

		_, err := ms.CreateAchievement(ctx, &types.MsgCreateAchievement{
			Authority:     addrStr,
			AchievementId: "ach_council",
			Name:          "Council Achievement",
		})

		require.NoError(t, err)

		// Verify achievement was created
		ach, err := f.keeper.Achievement.Get(ctx, "ach_council")
		require.NoError(t, err)
		require.Equal(t, "Council Achievement", ach.Name)
	})

	t.Run("authorized via governance authority", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.CreateAchievement(ctx, &types.MsgCreateAchievement{
			Authority:     authority,
			AchievementId: "gov_ach",
			Name:          "Governance Achievement",
		})

		require.NoError(t, err)
	})

	t.Run("empty achievement id", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.CreateAchievement(ctx, &types.MsgCreateAchievement{
			Authority:     authority,
			AchievementId: "",
			Name:          "Test",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidAchievementId)
	})

	t.Run("empty name", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.CreateAchievement(ctx, &types.MsgCreateAchievement{
			Authority:     authority,
			AchievementId: "ach1",
			Name:          "",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "achievement name cannot be empty")
	})

	t.Run("achievement already exists", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create existing achievement
		existing := types.Achievement{
			AchievementId: "existing",
			Name:          "Existing",
		}
		k.Achievement.Set(ctx, "existing", existing)

		_, err := ms.CreateAchievement(ctx, &types.MsgCreateAchievement{
			Authority:     authority,
			AchievementId: "existing",
			Name:          "New Name",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAchievementExists)
	})
}
