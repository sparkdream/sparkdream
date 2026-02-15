package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	sdkstore "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// initFixtureNoCommons creates a test fixture without a CommonsKeeper wired,
// so that IsCouncilAuthorized falls back to IsGovAuthority.
func initFixtureNoCommons(t *testing.T) *fixture {
	t.Helper()

	storeKey := sdkstore.NewKVStoreKey(types.StoreKey)
	memStoreKey := sdkstore.NewMemoryStoreKey("mem_name")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, sdkstore.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdkstore.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, cmtproto.Header{}, false, log.NewNopLogger())
	ctx = ctx.WithBlockTime(time.Now())

	cdc := codectestutil.CodecOptions{}.NewCodec()
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	authority := sdk.AccAddress([]byte("authority"))

	councilAddr := sdk.AccAddress([]byte("council_policy_addr_"))
	councilAddrStr := councilAddr.String()

	mockBK := NewMockBankKeeper()
	mockGK := &MockGroupKeeperReg{members: make(map[string]bool)}
	mockRK := NewMockRepKeeper()

	storeService := runtime.NewKVStoreService(storeKey)

	k := keeper.NewKeeper(
		storeService,
		cdc,
		addressCodec,
		authority,
		mockBK, // BankKeeper
		nil,    // CommonsKeeper (nil => falls back to IsGovAuthority)
		mockGK, // GroupKeeper
		mockRK, // RepKeeper
	)

	err := k.Params.Set(ctx, types.DefaultParams())
	require.NoError(t, err)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		mockBank:     mockBK,
		mockCommons:  nil,
		mockGroup:    mockGK,
		mockRep:      mockRK,
		councilAddr:  councilAddrStr,
	}
}

// mockCommonsKeeperAuthorized is a mock that always returns true for IsCouncilAuthorized.
type mockCommonsKeeperAuthorized struct {
	MockCommonsKeeper
}

func (m *mockCommonsKeeperAuthorized) IsCouncilAuthorized(_ context.Context, _ string, _ string, _ string) bool {
	return true
}

// initFixtureCouncilAuthorized creates a test fixture with a CommonsKeeper that
// authorizes any address via IsCouncilAuthorized.
func initFixtureCouncilAuthorized(t *testing.T) *fixture {
	t.Helper()

	storeKey := sdkstore.NewKVStoreKey(types.StoreKey)
	memStoreKey := sdkstore.NewMemoryStoreKey("mem_name")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, sdkstore.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdkstore.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, cmtproto.Header{}, false, log.NewNopLogger())
	ctx = ctx.WithBlockTime(time.Now())

	cdc := codectestutil.CodecOptions{}.NewCodec()
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	authority := sdk.AccAddress([]byte("authority"))

	councilAddr := sdk.AccAddress([]byte("council_policy_addr_"))
	councilAddrStr := councilAddr.String()

	mockBK := NewMockBankKeeper()
	mockGK := &MockGroupKeeperReg{members: make(map[string]bool)}
	mockCK := &mockCommonsKeeperAuthorized{MockCommonsKeeper: *NewMockCommonsKeeper()}
	mockRK := NewMockRepKeeper()

	storeService := runtime.NewKVStoreService(storeKey)

	k := keeper.NewKeeper(
		storeService,
		cdc,
		addressCodec,
		authority,
		mockBK, // BankKeeper
		mockCK, // CommonsKeeper (authorized)
		mockGK, // GroupKeeper
		mockRK, // RepKeeper
	)

	err := k.Params.Set(ctx, types.DefaultParams())
	require.NoError(t, err)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		mockBank:     mockBK,
		mockCommons:  nil, // not used directly in this fixture
		mockGroup:    mockGK,
		mockRep:      mockRK,
		councilAddr:  councilAddrStr,
	}
}

func TestUpdateOperationalParams(t *testing.T) {
	validOp := types.DefaultNameOperationalParams()

	// Modified operational params to verify the update actually takes effect.
	modifiedOp := types.NameOperationalParams{
		ExpirationDuration:   time.Hour * 24 * 180, // 180 days
		RegistrationFee:      sdk.NewCoin("uspark", math.NewInt(20000000)),
		DisputeStakeDream:    math.NewInt(75),
		DisputeTimeoutBlocks: 200000,
		ContestStakeDream:    math.NewInt(150),
	}

	tests := []struct {
		name      string
		setup     func(t *testing.T) (*fixture, string) // returns fixture and authority string
		opParams  types.NameOperationalParams
		expectErr bool
		errMsg    string
		postCheck func(t *testing.T, f *fixture)
	}{
		{
			name: "gov authority succeeds (no commons keeper)",
			setup: func(t *testing.T) (*fixture, string) {
				f := initFixtureNoCommons(t)
				authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
				require.NoError(t, err)
				return f, authorityStr
			},
			opParams:  modifiedOp,
			expectErr: false,
			postCheck: func(t *testing.T, f *fixture) {
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				require.Equal(t, modifiedOp.ExpirationDuration, params.ExpirationDuration)
				require.Equal(t, modifiedOp.RegistrationFee, params.RegistrationFee)
				require.True(t, modifiedOp.DisputeStakeDream.Equal(params.DisputeStakeDream))
				require.Equal(t, modifiedOp.DisputeTimeoutBlocks, params.DisputeTimeoutBlocks)
				require.True(t, modifiedOp.ContestStakeDream.Equal(params.ContestStakeDream))
			},
		},
		{
			name: "council authorized succeeds (commons keeper returns true)",
			setup: func(t *testing.T) (*fixture, string) {
				f := initFixtureCouncilAuthorized(t)
				// Use an arbitrary address; the mock authorizes everyone.
				operationsAddr := sdk.AccAddress([]byte("operations_member__"))
				addrStr, err := f.addressCodec.BytesToString(operationsAddr)
				require.NoError(t, err)
				return f, addrStr
			},
			opParams:  modifiedOp,
			expectErr: false,
			postCheck: func(t *testing.T, f *fixture) {
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				require.Equal(t, modifiedOp.ExpirationDuration, params.ExpirationDuration)
				require.Equal(t, modifiedOp.RegistrationFee, params.RegistrationFee)
				require.True(t, modifiedOp.DisputeStakeDream.Equal(params.DisputeStakeDream))
				require.Equal(t, modifiedOp.DisputeTimeoutBlocks, params.DisputeTimeoutBlocks)
				require.True(t, modifiedOp.ContestStakeDream.Equal(params.ContestStakeDream))
			},
		},
		{
			name: "unauthorized address fails",
			setup: func(t *testing.T) (*fixture, string) {
				f := initFixtureNoCommons(t)
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
			name: "invalid params - negative expiration duration",
			setup: func(t *testing.T) (*fixture, string) {
				f := initFixtureNoCommons(t)
				authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
				require.NoError(t, err)
				return f, authorityStr
			},
			opParams: types.NameOperationalParams{
				ExpirationDuration:   -time.Hour, // invalid
				RegistrationFee:      sdk.NewCoin("uspark", math.NewInt(10000000)),
				DisputeStakeDream:    math.NewInt(50),
				DisputeTimeoutBlocks: 100800,
				ContestStakeDream:    math.NewInt(100),
			},
			expectErr: true,
			errMsg:    "expiration duration must be positive",
		},
		{
			name: "invalid params - negative dispute stake",
			setup: func(t *testing.T) (*fixture, string) {
				f := initFixtureNoCommons(t)
				authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
				require.NoError(t, err)
				return f, authorityStr
			},
			opParams: types.NameOperationalParams{
				ExpirationDuration:   time.Hour * 24 * 365,
				RegistrationFee:      sdk.NewCoin("uspark", math.NewInt(10000000)),
				DisputeStakeDream:    math.NewInt(-1), // invalid
				DisputeTimeoutBlocks: 100800,
				ContestStakeDream:    math.NewInt(100),
			},
			expectErr: true,
			errMsg:    "dispute stake must be non-negative",
		},
		{
			name: "governance-only fields preserved after operational update",
			setup: func(t *testing.T) (*fixture, string) {
				f := initFixtureNoCommons(t)
				authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
				require.NoError(t, err)

				// Set custom governance-only fields before the operational update.
				customParams := types.DefaultParams()
				customParams.BlockedNames = []string{"reserved_phoenix", "reserved_aurora"}
				customParams.MinNameLength = 5
				customParams.MaxNameLength = 20
				customParams.MaxNamesPerAddress = 3
				err = f.keeper.Params.Set(f.ctx, customParams)
				require.NoError(t, err)

				return f, authorityStr
			},
			opParams:  modifiedOp,
			expectErr: false,
			postCheck: func(t *testing.T, f *fixture) {
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)

				// Governance-only fields must be preserved.
				require.Equal(t, []string{"reserved_phoenix", "reserved_aurora"}, params.BlockedNames)
				require.Equal(t, uint64(5), params.MinNameLength)
				require.Equal(t, uint64(20), params.MaxNameLength)
				require.Equal(t, uint64(3), params.MaxNamesPerAddress)

				// Operational fields must be updated.
				require.Equal(t, modifiedOp.ExpirationDuration, params.ExpirationDuration)
				require.Equal(t, modifiedOp.RegistrationFee, params.RegistrationFee)
				require.True(t, modifiedOp.DisputeStakeDream.Equal(params.DisputeStakeDream))
				require.Equal(t, modifiedOp.DisputeTimeoutBlocks, params.DisputeTimeoutBlocks)
				require.True(t, modifiedOp.ContestStakeDream.Equal(params.ContestStakeDream))
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
