package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	module "sparkdream/x/session/module"
	"sparkdream/x/session/types"
)

// --- Mock Keepers ---

type mockBankKeeper struct {
	SpendableCoinsFn               func(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFn                    func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModuleFn func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

func (m *mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	if m.SpendableCoinsFn != nil {
		return m.SpendableCoinsFn(ctx, addr)
	}
	return sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000_000))
}

func (m *mockBankKeeper) SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.SendCoinsFn != nil {
		return m.SendCoinsFn(ctx, fromAddr, toAddr, amt)
	}
	return nil
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.SendCoinsFromAccountToModuleFn != nil {
		return m.SendCoinsFromAccountToModuleFn(ctx, senderAddr, recipientModule, amt)
	}
	return nil
}

type mockAuthKeeper struct {
	addressCodec       address.Codec
	GetAccountFn       func(context.Context, sdk.AccAddress) sdk.AccountI
	GetModuleAddressFn func(name string) sdk.AccAddress
}

func (m *mockAuthKeeper) AddressCodec() address.Codec {
	return m.addressCodec
}

func (m *mockAuthKeeper) GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI {
	if m.GetAccountFn != nil {
		return m.GetAccountFn(ctx, addr)
	}
	return nil
}

func (m *mockAuthKeeper) GetModuleAddress(name string) sdk.AccAddress {
	if m.GetModuleAddressFn != nil {
		return m.GetModuleAddressFn(name)
	}
	return authtypes.NewModuleAddress(name)
}

// --- Fixture ---

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	bankKeeper   *mockBankKeeper
	authKeeper   *mockAuthKeeper
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	bk := &mockBankKeeper{}
	ak := &mockAuthKeeper{addressCodec: addressCodec}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		bk,
		ak,
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		bankKeeper:   bk,
		authKeeper:   ak,
	}
}

// testAddr generates a deterministic test address from a seed string.
func testAddr(seed string, codec address.Codec) string {
	addr := make([]byte, 20)
	copy(addr, seed)
	s, _ := codec.BytesToString(addr)
	return s
}

// createTestSession is a helper that stores a session with all indexes.
func createTestSession(t *testing.T, f *fixture, granter, grantee string, allowedTypes []string, expiration time.Time) types.Session {
	t.Helper()

	session := types.Session{
		Granter:         granter,
		Grantee:         grantee,
		AllowedMsgTypes: allowedTypes,
		SpendLimit:      sdk.NewInt64Coin("uspark", 10_000_000),
		Spent:           sdk.NewInt64Coin("uspark", 0),
		Expiration:      expiration,
		CreatedAt:       time.Now().UTC(),
		LastUsedAt:      time.Now().UTC(),
		ExecCount:       0,
		MaxExecCount:    0,
	}

	key := collections.Join(granter, grantee)
	require.NoError(t, f.keeper.Sessions.Set(f.ctx, key, session))
	require.NoError(t, f.keeper.SessionsByGranter.Set(f.ctx, collections.Join(granter, grantee)))
	require.NoError(t, f.keeper.SessionsByGrantee.Set(f.ctx, collections.Join(grantee, granter)))
	require.NoError(t, f.keeper.SessionsByExpiration.Set(f.ctx, collections.Join3(expiration.Unix(), granter, grantee)))

	return session
}

// --- Keeper method tests ---

func TestGetSession(t *testing.T) {
	f := initFixture(t)
	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// Not found
	_, err := f.keeper.GetSession(f.ctx, granter, grantee)
	require.Error(t, err)

	// Create session, then find it
	exp := time.Now().Add(24 * time.Hour).UTC()
	createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], exp)

	session, err := f.keeper.GetSession(f.ctx, granter, grantee)
	require.NoError(t, err)
	require.Equal(t, granter, session.Granter)
	require.Equal(t, grantee, session.Grantee)
}

func TestUpdateSessionSpent(t *testing.T) {
	f := initFixture(t)
	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	exp := time.Now().Add(24 * time.Hour).UTC()
	createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], exp)

	// Update spent
	fee := sdk.NewInt64Coin("uspark", 5000)
	require.NoError(t, f.keeper.UpdateSessionSpent(f.ctx, granter, grantee, fee))

	// Verify
	session, err := f.keeper.GetSession(f.ctx, granter, grantee)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt64Coin("uspark", 5000), session.Spent)

	// Update again
	require.NoError(t, f.keeper.UpdateSessionSpent(f.ctx, granter, grantee, fee))
	session, err = f.keeper.GetSession(f.ctx, granter, grantee)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt64Coin("uspark", 10000), session.Spent)
}

func TestUpdateSessionSpentNotFound(t *testing.T) {
	f := initFixture(t)
	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	err := f.keeper.UpdateSessionSpent(f.ctx, granter, grantee, sdk.NewInt64Coin("uspark", 100))
	require.Error(t, err)
}

func TestGetAuthority(t *testing.T) {
	f := initFixture(t)
	authority := f.keeper.GetAuthority()
	require.NotNil(t, authority)

	expected := authtypes.NewModuleAddress(types.GovModuleName)
	require.Equal(t, expected.Bytes(), authority)
}
