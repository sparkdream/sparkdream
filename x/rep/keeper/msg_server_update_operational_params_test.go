package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	module "sparkdream/x/rep/module"
	"sparkdream/x/rep/types"
)

// initFixtureNilCommons creates a fixture without a commons keeper,
// so IsCouncilAuthorized falls back to IsGovAuthority.
func initFixtureNilCommons(t *testing.T) *fixture {
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
		nil, // authKeeper
		nil, // bankKeeper
		nil, // commonsKeeper (nil - falls back to IsGovAuthority)
		nil, // seasonKeeper
	)

	genState := types.DefaultGenesis()
	if err := k.InitGenesis(ctx, *genState); err != nil {
		t.Fatalf("failed to init genesis: %v", err)
	}

	return &fixture{
		ctx:           ctx,
		keeper:        k,
		addressCodec:  addressCodec,
		commonsKeeper: nil,
		seasonKeeper:  nil,
	}
}

func TestMsgServerUpdateOperationalParams(t *testing.T) {
	t.Run("gov authority succeeds (nil commonsKeeper fallback)", func(t *testing.T) {
		f := initFixtureNilCommons(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr := f.keeper.GetAuthorityString()

		resp, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: types.DefaultRepOperationalParams(),
		})

		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify params were applied
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, int64(14400), params.EpochBlocks)
		require.Equal(t, uint32(5), params.JurySize)
	})

	t.Run("council authorized succeeds (mock commonsKeeper)", func(t *testing.T) {
		// Use a random address that is NOT the gov authority
		randomAddr := sdk.AccAddress([]byte("council_operator"))

		// Build fixture with commonsKeeper that authorizes this address
		commonsKeeperMock := &mockCommonsKeeper{
			IsCouncilAuthorizedFn: func(ctx context.Context, addr string, council string, committee string) bool {
				return true // Authorize everyone via council
			},
		}

		f := initFixture(t)
		// Override the commonsKeeper's IsCouncilAuthorized to always authorize
		f.commonsKeeper.IsCouncilAuthorizedFn = commonsKeeperMock.IsCouncilAuthorizedFn

		ms := keeper.NewMsgServerImpl(f.keeper)

		randomAddrStr, err := f.addressCodec.BytesToString(randomAddr)
		require.NoError(t, err)

		resp, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         randomAddrStr,
			OperationalParams: types.DefaultRepOperationalParams(),
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("unauthorized fails", func(t *testing.T) {
		// Use fixture with mock that always denies via IsCouncilAuthorized
		f := initFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(ctx context.Context, addr string, council string, committee string) bool {
			return false
		}
		ms := keeper.NewMsgServerImpl(f.keeper)

		nonAuthority := sdk.AccAddress([]byte("random_user_____"))
		nonAuthorityStr, err := f.addressCodec.BytesToString(nonAuthority)
		require.NoError(t, err)

		_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         nonAuthorityStr,
			OperationalParams: types.DefaultRepOperationalParams(),
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidSigner)
		require.Contains(t, err.Error(), "not authorized")
	})

	t.Run("invalid params fails (JurySize even)", func(t *testing.T) {
		f := initFixtureNilCommons(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr := f.keeper.GetAuthorityString()

		// Use default params but set JurySize to 4 (even - must be odd)
		opParams := types.DefaultRepOperationalParams()
		opParams.JurySize = 4

		_, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: opParams,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "jury size must be odd")
	})

	t.Run("governance-only fields preserved", func(t *testing.T) {
		f := initFixtureNilCommons(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr := f.keeper.GetAuthorityString()

		// Set custom governance-only fields before the operational update
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)

		// Modify governance-only fields to non-default values
		params.CompleterShare = math.LegacyNewDecWithPrec(80, 2)          // 0.80
		params.TreasuryShare = math.LegacyNewDecWithPrec(20, 2)           // 0.20
		params.ConvictionHalfLifeEpochs = 14                              // non-default
		params.ExternalConvictionRatio = math.LegacyNewDecWithPrec(60, 2) // 0.60

		err = f.keeper.Params.Set(f.ctx, params)
		require.NoError(t, err)

		// Apply operational params update
		resp, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: types.DefaultRepOperationalParams(),
		})

		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify governance-only fields are preserved
		updatedParams, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)

		require.True(t, updatedParams.CompleterShare.Equal(math.LegacyNewDecWithPrec(80, 2)),
			"CompleterShare should be preserved at 0.80, got %s", updatedParams.CompleterShare)
		require.True(t, updatedParams.TreasuryShare.Equal(math.LegacyNewDecWithPrec(20, 2)),
			"TreasuryShare should be preserved at 0.20, got %s", updatedParams.TreasuryShare)
		require.Equal(t, int64(14), updatedParams.ConvictionHalfLifeEpochs,
			"ConvictionHalfLifeEpochs should be preserved")
		require.True(t, updatedParams.ExternalConvictionRatio.Equal(math.LegacyNewDecWithPrec(60, 2)),
			"ExternalConvictionRatio should be preserved at 0.60, got %s", updatedParams.ExternalConvictionRatio)

		// Verify operational fields were updated (to default values)
		require.Equal(t, int64(14400), updatedParams.EpochBlocks)
		require.Equal(t, uint32(5), updatedParams.JurySize)
		require.True(t, updatedParams.StakingApy.Equal(math.LegacyNewDecWithPrec(10, 2)),
			"StakingApy should be updated to default 10%%")
	})
}
