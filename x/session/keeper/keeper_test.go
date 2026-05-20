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
	SendCoinsFromModuleToAccountFn func(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModuleFn  func(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
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

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.SendCoinsFromModuleToAccountFn != nil {
		return m.SendCoinsFromModuleToAccountFn(ctx, senderModule, recipientAddr, amt)
	}
	return nil
}

func (m *mockBankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	if m.SendCoinsFromModuleToModuleFn != nil {
		return m.SendCoinsFromModuleToModuleFn(ctx, senderModule, recipientModule, amt)
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

// createTestSession is a helper that stores a session as a SESSION_KEY-type
// Grant with MaxExecCount = 0 (unlimited). For tests that need a specific
// cap, use createTestSessionWithExec.
func createTestSession(t *testing.T, f *fixture, granter, grantee string, allowedTypes []string, expiration time.Time) types.Session {
	t.Helper()
	return createTestSessionWithExec(t, f, granter, grantee, allowedTypes, expiration, 0)
}

// createTestSessionWithExec is like createTestSession but takes a maxExec
// override.
func createTestSessionWithExec(t *testing.T, f *fixture, granter, grantee string, allowedTypes []string, expiration time.Time, maxExec uint64) types.Session {
	t.Helper()

	now := time.Now().UTC()
	id, err := f.keeper.GrantSeq.Next(f.ctx)
	require.NoError(t, err)
	id++ // match nextGrantID's 1-indexed convention

	g := types.Grant{
		Id:        id,
		Granter:   granter,
		Grantee:   grantee,
		Type:      types.GrantType_GRANT_TYPE_SESSION_KEY,
		Status:    types.GrantStatus_GRANT_STATUS_ACTIVE,
		CreatedAt: now,
		ExpiresAt: expiration,
		Payload: &types.Grant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: allowedTypes,
				SpendLimit:      sdk.NewInt64Coin("uspark", 10_000_000),
				Spent:           sdk.NewInt64Coin("uspark", 0),
				LastUsedAt:      now,
				ExecCount:       0,
				MaxExecCount:    maxExec,
			},
		},
	}

	require.NoError(t, f.keeper.Grants.Set(f.ctx, id, g))
	require.NoError(t, f.keeper.GrantsByGranter.Set(f.ctx, collections.Join(granter, id)))
	require.NoError(t, f.keeper.GrantsByGrantee.Set(f.ctx, collections.Join(grantee, id)))
	require.NoError(t, f.keeper.GrantsByExpiration.Set(f.ctx, collections.Join(expiration.Unix(), id)))
	require.NoError(t, f.keeper.GrantsByTypeAndGranter.Set(f.ctx,
		collections.Join3(int32(types.GrantType_GRANT_TYPE_SESSION_KEY), granter, id)))
	require.NoError(t, f.keeper.SessionKeyByPair.Set(f.ctx, collections.Join(granter, grantee), id))

	// Bump active-grant count to match the active-status invariant.
	ckey := collections.Join(granter, int32(types.GrantType_GRANT_TYPE_SESSION_KEY))
	cur, _ := f.keeper.ActiveGrantCountByType.Get(f.ctx, ckey)
	require.NoError(t, f.keeper.ActiveGrantCountByType.Set(f.ctx, ckey, cur+1))

	return types.Session{
		Granter:         granter,
		Grantee:         grantee,
		AllowedMsgTypes: allowedTypes,
		SpendLimit:      sdk.NewInt64Coin("uspark", 10_000_000),
		Spent:           sdk.NewInt64Coin("uspark", 0),
		Expiration:      expiration,
		CreatedAt:       now,
		LastUsedAt:      now,
		ExecCount:       0,
		MaxExecCount:    maxExec,
	}
}

// cleanupSessionPair removes any SESSION_KEY grant for the (granter,
// grantee) pair and clears every secondary index entry. Intended for
// table-driven tests that need to set up and tear down a session across
// sub-tests without re-initializing the keeper.
func cleanupSessionPair(t *testing.T, f *fixture, granter, grantee string) {
	t.Helper()
	id, err := f.keeper.SessionKeyByPair.Get(f.ctx, collections.Join(granter, grantee))
	if err != nil {
		return
	}
	g, err := f.keeper.Grants.Get(f.ctx, id)
	if err != nil {
		_ = f.keeper.SessionKeyByPair.Remove(f.ctx, collections.Join(granter, grantee))
		return
	}
	_ = f.keeper.Grants.Remove(f.ctx, id)
	_ = f.keeper.GrantsByGranter.Remove(f.ctx, collections.Join(granter, id))
	_ = f.keeper.GrantsByGrantee.Remove(f.ctx, collections.Join(grantee, id))
	_ = f.keeper.GrantsByExpiration.Remove(f.ctx, collections.Join(g.ExpiresAt.Unix(), id))
	_ = f.keeper.GrantsByTypeAndGranter.Remove(f.ctx,
		collections.Join3(int32(types.GrantType_GRANT_TYPE_SESSION_KEY), granter, id))
	_ = f.keeper.SessionKeyByPair.Remove(f.ctx, collections.Join(granter, grantee))

	ckey := collections.Join(granter, int32(types.GrantType_GRANT_TYPE_SESSION_KEY))
	cur, err := f.keeper.ActiveGrantCountByType.Get(f.ctx, ckey)
	if err == nil && cur > 0 {
		if cur == 1 {
			_ = f.keeper.ActiveGrantCountByType.Remove(f.ctx, ckey)
		} else {
			_ = f.keeper.ActiveGrantCountByType.Set(f.ctx, ckey, cur-1)
		}
	}
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
