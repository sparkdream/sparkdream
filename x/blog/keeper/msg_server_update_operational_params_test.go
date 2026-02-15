package keeper_test

import (
	"context"
	"testing"

	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	module "sparkdream/x/blog/module"
	"sparkdream/x/blog/types"
)

// mockCommonsKeeper implements types.CommonsKeeper for testing
type mockCommonsKeeper struct {
	IsCouncilAuthorizedFn func(ctx context.Context, addr string, council string, committee string) bool
}

func (m *mockCommonsKeeper) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if m.IsCouncilAuthorizedFn != nil {
		return m.IsCouncilAuthorizedFn(ctx, addr, council, committee)
	}
	return false
}

// setupMsgServerWithCommons creates a keeper with a custom CommonsKeeper wired in.
func setupMsgServerWithCommons(t testing.TB, commonsKeeper types.CommonsKeeper) (keeper.Keeper, types.MsgServer, sdk.Context) {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("sprkdrm")

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)

	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	bankKeeper := &mockBankKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		bankKeeper,
		commonsKeeper,
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return k, keeper.NewMsgServerImpl(k), ctx
}

func TestMsgUpdateOperationalParams(t *testing.T) {
	t.Run("gov authority succeeds", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)

		msg := &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: types.DefaultBlogOperationalParams(),
		}

		_, err = ms.UpdateOperationalParams(f.ctx, msg)
		require.NoError(t, err)

		// Verify the params were stored with correct operational values
		storedParams, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, types.DefaultBlogOperationalParams().CostPerByte, storedParams.CostPerByte)
		require.Equal(t, types.DefaultBlogOperationalParams().CostPerByteExempt, storedParams.CostPerByteExempt)
	})

	t.Run("council authorized succeeds", func(t *testing.T) {
		mock := &mockCommonsKeeper{
			IsCouncilAuthorizedFn: func(ctx context.Context, addr string, council string, committee string) bool {
				return council == "commons" && committee == "operations"
			},
		}
		k, ms, ctx := setupMsgServerWithCommons(t, mock)

		// Use an address that is NOT the gov authority
		addrCodec := addresscodec.NewBech32Codec("sprkdrm")
		randomAddr, err := addrCodec.BytesToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
		require.NoError(t, err)

		msg := &types.MsgUpdateOperationalParams{
			Authority:         randomAddr,
			OperationalParams: types.DefaultBlogOperationalParams(),
		}

		_, err = ms.UpdateOperationalParams(ctx, msg)
		require.NoError(t, err)

		// Verify params were stored
		storedParams, err := k.Params.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, types.DefaultBlogOperationalParams().CostPerByte, storedParams.CostPerByte)
	})

	t.Run("unauthorized fails", func(t *testing.T) {
		f := initFixture(t) // nil commonsKeeper => falls back to IsGovAuthority

		addrCodec := addresscodec.NewBech32Codec("sprkdrm")
		randomAddr, err := addrCodec.BytesToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
		require.NoError(t, err)

		ms := keeper.NewMsgServerImpl(f.keeper)

		msg := &types.MsgUpdateOperationalParams{
			Authority:         randomAddr,
			OperationalParams: types.DefaultBlogOperationalParams(),
		}

		_, err = ms.UpdateOperationalParams(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not authorized")
	})

	t.Run("governance-only fields preserved", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)

		// Set initial params with MaxTitleLength = 999 (governance-only field)
		initialParams := types.DefaultParams()
		initialParams.MaxTitleLength = 999
		require.NoError(t, f.keeper.Params.Set(f.ctx, initialParams))

		// Send operational params update (does not include MaxTitleLength)
		msg := &types.MsgUpdateOperationalParams{
			Authority:         authorityStr,
			OperationalParams: types.DefaultBlogOperationalParams(),
		}

		_, err = ms.UpdateOperationalParams(f.ctx, msg)
		require.NoError(t, err)

		// Verify governance-only field was preserved
		storedParams, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(999), storedParams.MaxTitleLength,
			"MaxTitleLength should still be 999 after operational params update")
	})
}
