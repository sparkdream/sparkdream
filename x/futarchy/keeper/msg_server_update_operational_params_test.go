package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/futarchy/keeper"
	module "sparkdream/x/futarchy/module"
	"sparkdream/x/futarchy/types"
)

// mockCommonsKeeperAuthorized always returns true for IsCouncilAuthorized.
type mockCommonsKeeperAuthorized struct{}

func (m *mockCommonsKeeperAuthorized) IsCouncilAuthorized(_ context.Context, _ string, _ string, _ string) bool {
	return true
}

// mockCommonsKeeperUnauthorized always returns false for IsCouncilAuthorized.
type mockCommonsKeeperUnauthorized struct{}

func (m *mockCommonsKeeperUnauthorized) IsCouncilAuthorized(_ context.Context, _ string, _ string, _ string) bool {
	return false
}

// opFixture is a lightweight test fixture for UpdateOperationalParams tests.
type opFixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
}

// initOpFixtureNoCommons creates a fixture without commons keeper (falls back to IsGovAuthority).
func initOpFixtureNoCommons(t *testing.T) *opFixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	mockBank := NewMockBankKeeper()
	mockAuth := &MockAuthKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		mockAuth,
		mockBank,
	)
	// No commonsKeeper set => falls back to IsGovAuthority

	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &opFixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
	}
}

// initOpFixtureWithCommons creates a fixture with a specific CommonsKeeper mock.
func initOpFixtureWithCommons(t *testing.T, commonsKeeper types.CommonsKeeper) *opFixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	mockBank := NewMockBankKeeper()
	mockAuth := &MockAuthKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		mockAuth,
		mockBank,
	)
	k.SetCommonsKeeper(commonsKeeper)

	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &opFixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
	}
}

func TestUpdateOperationalParams(t *testing.T) {
	validOp := types.DefaultFutarchyOperationalParams()

	// Modified operational params to verify the update takes effect.
	modifiedOp := types.FutarchyOperationalParams{
		TradingFeeBps:      50,      // 0.5%
		MaxDuration:        1000000, // shorter than default
		MaxRedemptionDelay: 2000000, // different from default
	}

	tests := []struct {
		name      string
		setup     func(t *testing.T) (*opFixture, string) // returns fixture and authority string
		opParams  types.FutarchyOperationalParams
		expectErr bool
		errMsg    string
		postCheck func(t *testing.T, f *opFixture)
	}{
		{
			name: "gov authority succeeds (no commons keeper)",
			setup: func(t *testing.T) (*opFixture, string) {
				f := initOpFixtureNoCommons(t)
				authority := authtypes.NewModuleAddress(types.GovModuleName)
				authorityStr, err := f.addressCodec.BytesToString(authority)
				require.NoError(t, err)
				return f, authorityStr
			},
			opParams:  modifiedOp,
			expectErr: false,
			postCheck: func(t *testing.T, f *opFixture) {
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				require.Equal(t, modifiedOp.TradingFeeBps, params.TradingFeeBps)
				require.Equal(t, modifiedOp.MaxDuration, params.MaxDuration)
				require.Equal(t, modifiedOp.MaxRedemptionDelay, params.MaxRedemptionDelay)
			},
		},
		{
			name: "council authorized succeeds (commons keeper returns true)",
			setup: func(t *testing.T) (*opFixture, string) {
				f := initOpFixtureWithCommons(t, &mockCommonsKeeperAuthorized{})
				// Use an arbitrary address; the mock authorizes everyone.
				operationsAddr := sdk.AccAddress([]byte("operations_member__"))
				addrStr, err := f.addressCodec.BytesToString(operationsAddr)
				require.NoError(t, err)
				return f, addrStr
			},
			opParams:  modifiedOp,
			expectErr: false,
			postCheck: func(t *testing.T, f *opFixture) {
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				require.Equal(t, modifiedOp.TradingFeeBps, params.TradingFeeBps)
				require.Equal(t, modifiedOp.MaxDuration, params.MaxDuration)
				require.Equal(t, modifiedOp.MaxRedemptionDelay, params.MaxRedemptionDelay)
			},
		},
		{
			name: "unauthorized address fails (commons returns false)",
			setup: func(t *testing.T) (*opFixture, string) {
				f := initOpFixtureWithCommons(t, &mockCommonsKeeperUnauthorized{})
				randomAddr := sdk.AccAddress([]byte("random_unauthorized_"))
				addrStr, err := f.addressCodec.BytesToString(randomAddr)
				require.NoError(t, err)
				return f, addrStr
			},
			opParams:  validOp,
			expectErr: true,
			errMsg:    "not authorized",
		},
		{
			name: "unauthorized address fails (no commons, not gov authority)",
			setup: func(t *testing.T) (*opFixture, string) {
				f := initOpFixtureNoCommons(t)
				randomAddr := sdk.AccAddress([]byte("random_unauthorized_"))
				addrStr, err := f.addressCodec.BytesToString(randomAddr)
				require.NoError(t, err)
				return f, addrStr
			},
			opParams:  validOp,
			expectErr: true,
			errMsg:    "not authorized",
		},
		{
			name: "invalid params - trading fee exceeds 10000 bps",
			setup: func(t *testing.T) (*opFixture, string) {
				f := initOpFixtureNoCommons(t)
				authority := authtypes.NewModuleAddress(types.GovModuleName)
				authorityStr, err := f.addressCodec.BytesToString(authority)
				require.NoError(t, err)
				return f, authorityStr
			},
			opParams: types.FutarchyOperationalParams{
				TradingFeeBps:      10001, // exceeds 100%
				MaxDuration:        5256000,
				MaxRedemptionDelay: 5256000,
			},
			expectErr: true,
			errMsg:    "trading_fee_bps must be <= 10000",
		},
		{
			name: "invalid params - non-positive max duration",
			setup: func(t *testing.T) (*opFixture, string) {
				f := initOpFixtureNoCommons(t)
				authority := authtypes.NewModuleAddress(types.GovModuleName)
				authorityStr, err := f.addressCodec.BytesToString(authority)
				require.NoError(t, err)
				return f, authorityStr
			},
			opParams: types.FutarchyOperationalParams{
				TradingFeeBps:      30,
				MaxDuration:        0, // invalid
				MaxRedemptionDelay: 5256000,
			},
			expectErr: true,
			errMsg:    "max_duration must be positive",
		},
		{
			name: "invalid params - negative max redemption delay",
			setup: func(t *testing.T) (*opFixture, string) {
				f := initOpFixtureNoCommons(t)
				authority := authtypes.NewModuleAddress(types.GovModuleName)
				authorityStr, err := f.addressCodec.BytesToString(authority)
				require.NoError(t, err)
				return f, authorityStr
			},
			opParams: types.FutarchyOperationalParams{
				TradingFeeBps:      30,
				MaxDuration:        5256000,
				MaxRedemptionDelay: -1, // invalid
			},
			expectErr: true,
			errMsg:    "max_redemption_delay must be non-negative",
		},
		{
			name: "governance-only fields preserved after operational update",
			setup: func(t *testing.T) (*opFixture, string) {
				f := initOpFixtureNoCommons(t)
				authority := authtypes.NewModuleAddress(types.GovModuleName)
				authorityStr, err := f.addressCodec.BytesToString(authority)
				require.NoError(t, err)

				// Set custom governance-only fields before the operational update.
				customParams := types.DefaultParams()
				customParams.MinLiquidity = types.DefaultMinLiquidity.MulRaw(2) // 200,000
				customParams.DefaultMinTick = types.DefaultMinTick.MulRaw(3)    // 3000
				customParams.MaxLmsrExponent = "50"
				err = f.keeper.Params.Set(f.ctx, customParams)
				require.NoError(t, err)

				return f, authorityStr
			},
			opParams:  modifiedOp,
			expectErr: false,
			postCheck: func(t *testing.T, f *opFixture) {
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)

				// Governance-only fields must be preserved.
				require.True(t, types.DefaultMinLiquidity.MulRaw(2).Equal(params.MinLiquidity),
					"MinLiquidity should be preserved: got %s", params.MinLiquidity)
				require.True(t, types.DefaultMinTick.MulRaw(3).Equal(params.DefaultMinTick),
					"DefaultMinTick should be preserved: got %s", params.DefaultMinTick)
				require.Equal(t, "50", params.MaxLmsrExponent)

				// Operational fields must be updated.
				require.Equal(t, modifiedOp.TradingFeeBps, params.TradingFeeBps)
				require.Equal(t, modifiedOp.MaxDuration, params.MaxDuration)
				require.Equal(t, modifiedOp.MaxRedemptionDelay, params.MaxRedemptionDelay)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, authorityStr := tc.setup(t)

			msgServer := keeper.NewMsgServerImpl(f.keeper)
			msg := &types.MsgUpdateOperationalParams{
				Authority:         authorityStr,
				OperationalParams: tc.opParams,
			}

			_, err := msgServer.UpdateOperationalParams(f.ctx, msg)
			if tc.expectErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					require.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
			}

			if tc.postCheck != nil && !tc.expectErr {
				tc.postCheck(t, f)
			}
		})
	}
}
