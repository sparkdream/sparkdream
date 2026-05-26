package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"
	ibctypes "github.com/cosmos/ibc-go/v10/modules/core/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/federation/keeper"
	module "sparkdream/x/federation/module"
	"sparkdream/x/federation/types"
)

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	authority    string
	repKeeper    *mockRepKeeper
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	mockUpgradeKeeper := newMockUpgradeKeeper()

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		&mockAuthKeeper{addressCodec: addressCodec},
		&mockBankKeeper{balances: make(map[string]sdk.Coins)},
		func() *ibckeeper.Keeper {
			return ibckeeper.NewKeeper(encCfg.Codec, storeService, newMockParams(), mockUpgradeKeeper, authority.String())
		},
	)

	// Wire mock commons keeper for authorization tests
	k.SetCommonsKeeper(&mockCommonsKeeper{})
	repKeeper := &mockRepKeeper{}
	k.SetRepKeeper(repKeeper)
	k.SetIdentityKeeper(&mockIdentityKeeper{})

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	authorityStr, _ := addressCodec.BytesToString(authority)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		authority:    authorityStr,
		repKeeper:    repKeeper,
	}
}

// --- Mock Keepers ---

type mockAuthKeeper struct {
	addressCodec address.Codec
}

func (m *mockAuthKeeper) AddressCodec() address.Codec {
	return m.addressCodec
}

func (m *mockAuthKeeper) GetAccount(_ context.Context, _ sdk.AccAddress) sdk.AccountI {
	return nil
}

func (m *mockAuthKeeper) GetModuleAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress(name)
}

type mockBankKeeper struct {
	balances map[string]sdk.Coins
}

func (m *mockBankKeeper) SpendableCoins(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balances[addr.String()]
}

func (m *mockBankKeeper) SendCoins(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
	return nil
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
	return nil
}

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
	return nil
}

func (m *mockBankKeeper) BurnCoins(_ context.Context, _ string, _ sdk.Coins) error {
	return nil
}

func (m *mockBankKeeper) SetDenomMetaData(_ context.Context, _ banktypes.Metadata) {}

func (m *mockBankKeeper) GetDenomMetaData(_ context.Context, _ string) (banktypes.Metadata, bool) {
	return banktypes.Metadata{}, false
}

// mockIdentityKeeper implements types.IdentityKeeper for testing.
// Always returns "uspark"/"udream" — the legacy denoms unit tests assume.
// E2E tests run against a real chain with the real identity keeper.
type mockIdentityKeeper struct{}

func (mockIdentityKeeper) IsIdentityKeeper()                       {}
func (m *mockIdentityKeeper) BondDenom(_ context.Context) string  { return "uspark" }
func (m *mockIdentityKeeper) DreamDenom(_ context.Context) string { return "udream" }

type mockCommonsKeeper struct {
	// IsGroupPolicyAddressFn lets specific tests override the default.
	// Default: every address is treated as a registered group policy
	// address, which lets tests exercise the happy path of any code
	// that takes a `controller_group` input without explicit setup.
	IsGroupPolicyAddressFn func(ctx context.Context, addr string) bool

	// GetCouncilPolicyAddressFn lets tests inject a specific policy
	// address. Default: returns a synthetic address so federation's
	// MsgRegisterBridge controller-resolution fall-back path works in
	// unit tests without standing up the full commons bootstrap.
	GetCouncilPolicyAddressFn func(ctx context.Context, council string, committee string) (string, bool)
}

func (m *mockCommonsKeeper) IsCouncilAuthorized(_ context.Context, addr string, _, _ string) bool {
	// In tests, authority is always authorized
	return true
}

func (m *mockCommonsKeeper) IsGroupPolicyAddress(ctx context.Context, addr string) bool {
	if m.IsGroupPolicyAddressFn != nil {
		return m.IsGroupPolicyAddressFn(ctx, addr)
	}
	return true
}

// testOpsCommPolicyAddress is the synthetic Operations Committee policy
// address returned by the default mockCommonsKeeper. Stable across the
// suite so tests can assert against the resolved controller.
const testOpsCommPolicyAddress = "cosmos1opcommopcommopcommopcommopcommop6vnq06"

func (m *mockCommonsKeeper) GetCouncilPolicyAddress(ctx context.Context, council string, committee string) (string, bool) {
	if m.GetCouncilPolicyAddressFn != nil {
		return m.GetCouncilPolicyAddressFn(ctx, council, committee)
	}
	if council == "commons" && committee == "operations" {
		return testOpsCommPolicyAddress, true
	}
	return "", false
}

type mockRepKeeper struct {
	// In-memory BondedRole state keyed by (roleType, addr). Tests seed this
	// via SeedBondedRole to simulate a verifier having bonded via x/rep.
	bondedRoles map[string]reptypes.BondedRole
	// BondedRoleConfig write-throughs land here keyed by role_type; the
	// bonded_role_sync tests assert against this map.
	bondedRoleConfigs map[reptypes.RoleType]reptypes.BondedRoleConfig
}

// SeedBondedRole inserts a BondedRole record into the mock keyed by
// (roleType, addr). Used by tests to stand in for a verifier having bonded
// via x/rep before federation runs its handlers.
func (m *mockRepKeeper) SeedBondedRole(roleType reptypes.RoleType, addr string, role reptypes.BondedRole) {
	if m.bondedRoles == nil {
		m.bondedRoles = make(map[string]reptypes.BondedRole)
	}
	m.bondedRoles[mockBondedRoleKey(roleType, addr)] = role
}

func mockBondedRoleKey(roleType reptypes.RoleType, addr string) string {
	return reptypes.RoleType_name[int32(roleType)] + "/" + addr
}

func (m *mockRepKeeper) GetTrustLevel(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
	return reptypes.TrustLevel(3), nil // TRUSTED
}

func (m *mockRepKeeper) MintDREAM(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
	return nil
}

func (m *mockRepKeeper) BurnDREAM(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
	return nil
}

func (m *mockRepKeeper) LockDREAM(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
	return nil
}

func (m *mockRepKeeper) UnlockDREAM(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
	return nil
}

// --- BondedRole stubs (verifier is ROLE_TYPE_FEDERATION_VERIFIER). ---

func (m *mockRepKeeper) GetBondedRole(_ context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error) {
	if br, ok := m.bondedRoles[mockBondedRoleKey(roleType, addr)]; ok {
		return br, nil
	}
	return reptypes.BondedRole{}, reptypes.ErrBondedRoleNotFound
}

func (m *mockRepKeeper) ReserveBond(_ context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	current, _ := math.NewIntFromString(br.CurrentBond)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	if committed.IsNil() {
		committed = math.ZeroInt()
	}
	if current.Sub(committed).LT(amount) {
		return reptypes.ErrInsufficientBond
	}
	br.TotalCommittedBond = committed.Add(amount).String()
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) ReleaseBond(_ context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	if committed.IsNil() {
		committed = math.ZeroInt()
	}
	released := committed.Sub(amount)
	if released.IsNegative() {
		released = math.ZeroInt()
	}
	br.TotalCommittedBond = released.String()
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) SlashBond(_ context.Context, roleType reptypes.RoleType, addr string, amount math.Int, _ string) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	current, _ := math.NewIntFromString(br.CurrentBond)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	if committed.IsNil() {
		committed = math.ZeroInt()
	}
	slash := amount
	if slash.GT(current) {
		slash = current
	}
	br.CurrentBond = current.Sub(slash).String()
	released := committed.Sub(slash)
	if released.IsNegative() {
		released = math.ZeroInt()
	}
	br.TotalCommittedBond = released.String()
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) IncreaseBond(_ context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	current, _ := math.NewIntFromString(br.CurrentBond)
	if current.IsNil() {
		current = math.ZeroInt()
	}
	br.CurrentBond = current.Add(amount).String()
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) RecordRewardPayout(_ context.Context, roleType reptypes.RoleType, addr string, epoch int64, amount math.Int) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return nil
	}
	prev, _ := math.NewIntFromString(br.CumulativeRewards)
	if prev.IsNil() {
		prev = math.ZeroInt()
	}
	br.CumulativeRewards = prev.Add(amount).String()
	br.LastRewardEpoch = epoch
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) RecordActivity(_ context.Context, _ reptypes.RoleType, _ string) error {
	return nil
}

func (m *mockRepKeeper) SetBondStatus(_ context.Context, roleType reptypes.RoleType, addr string, status reptypes.BondedRoleStatus, cooldownUntil int64) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	br.BondStatus = status
	br.DemotionCooldownUntil = cooldownUntil
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) SetBondedRoleConfig(_ context.Context, cfg reptypes.BondedRoleConfig) error {
	if m.bondedRoleConfigs == nil {
		m.bondedRoleConfigs = make(map[reptypes.RoleType]reptypes.BondedRoleConfig)
	}
	m.bondedRoleConfigs[cfg.RoleType] = cfg
	return nil
}

// --- IBC Mocks ---

type mockUpgradeKeeper struct {
	clienttypes.UpgradeKeeper
	initialized bool
}

func (m mockUpgradeKeeper) GetUpgradePlan(_ context.Context) (upgradetypes.Plan, error) {
	return upgradetypes.Plan{}, nil
}

func newMockUpgradeKeeper() *mockUpgradeKeeper {
	return &mockUpgradeKeeper{initialized: true}
}

type mockParams struct {
	ibctypes.ParamSubspace
	initialized bool
}

func newMockParams() *mockParams {
	return &mockParams{initialized: true}
}

func (mockParams) GetParamSet(_ sdk.Context, _ paramtypes.ParamSet) {}

// --- Test Helpers ---

// registerTestPeer registers an ActivityPub peer with inbound blog_post allowed.
func registerTestPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority:   f.authority,
		PeerId:      peerID,
		DisplayName: "Test Peer " + peerID,
		Type:        types.PeerType_PEER_TYPE_ACTIVITYPUB,
	})
	require.NoError(t, err)
	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority,
		PeerId:    peerID,
		Policy: types.PeerPolicy{
			InboundContentTypes:  []string{"blog_post", "forum_thread"},
			OutboundContentTypes: []string{"blog_post"},
		},
	})
	require.NoError(t, err)
}

// registerTestNOSTRPeer registers a NOSTR relay peer with inbound blog_post allowed.
func registerTestNOSTRPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority:   f.authority,
		PeerId:      peerID,
		DisplayName: "NOSTR Relay " + peerID,
		Type:        types.PeerType_PEER_TYPE_NOSTR,
	})
	require.NoError(t, err)
	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority,
		PeerId:    peerID,
		Policy: types.PeerPolicy{
			InboundContentTypes:  []string{"blog_post", "forum_thread"},
			OutboundContentTypes: []string{"blog_post"},
		},
	})
	require.NoError(t, err)
}

// registerTestLENSPeer registers a Lens Chain peer with inbound blog_post allowed.
func registerTestLENSPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority:   f.authority,
		PeerId:      peerID,
		DisplayName: "Lens deployment " + peerID,
		Type:        types.PeerType_PEER_TYPE_LENS,
	})
	require.NoError(t, err)
	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority,
		PeerId:    peerID,
		Policy: types.PeerPolicy{
			InboundContentTypes:  []string{"blog_post", "forum_thread"},
			OutboundContentTypes: []string{"blog_post"},
		},
	})
	require.NoError(t, err)
}

// registerTestIBCPeer registers a Spark Dream IBC peer.
func registerTestIBCPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority:    f.authority,
		PeerId:       peerID,
		DisplayName:  "IBC Peer " + peerID,
		Type:         types.PeerType_PEER_TYPE_SPARK_DREAM,
		IbcChannelId: "channel-0",
	})
	require.NoError(t, err)
}

// testAddr returns a deterministic test address string.
func testAddr(t *testing.T, f *fixture, seed string) string {
	t.Helper()
	// Pad seed to exactly 20 bytes for AccAddress
	padded := seed
	for len(padded) < 20 {
		padded += "_"
	}
	addr := sdk.AccAddress([]byte(padded[:20]))
	str, err := f.addressCodec.BytesToString(addr)
	require.NoError(t, err)
	return str
}

// registerTestBridge registers a bridge operator for a peer and returns the operator address string.
func registerTestBridge(t *testing.T, f *fixture, ms types.MsgServer, peerID, operatorSeed string) string {
	t.Helper()
	operatorStr := testAddr(t, f, operatorSeed)
	_, err := ms.RegisterBridge(f.ctx, &types.MsgRegisterBridge{Operator: operatorStr,
		PeerId:   peerID,
		Protocol: "activitypub",
		Endpoint: "https://" + operatorSeed + ".example.com",
	})
	require.NoError(t, err)
	return operatorStr
}

// submitTestContent submits content and returns the content ID.
func submitTestContent(t *testing.T, f *fixture, ms types.MsgServer, operatorStr, peerID string, hash []byte) uint64 {
	t.Helper()
	resp, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		Operator:        operatorStr,
		PeerId:          peerID,
		RemoteContentId: "post-" + string(hash[:4]),
		ContentType:     "blog_post",
		CreatorIdentity: "@test@example.com",
		Title:           "Test",
		Body:            "Content",
		ContentHash:     hash,
	})
	require.NoError(t, err)
	return resp.ContentId
}

// bondTestVerifier seeds a BondedRole(ROLE_TYPE_FEDERATION_VERIFIER, addr)
// in the mock rep keeper, standing in for what x/rep's MsgBondRole would do.
// Returns the bonded address string.
func bondTestVerifier(t *testing.T, f *fixture, _ types.MsgServer, seed string) string {
	t.Helper()
	addr := testAddr(t, f, seed)
	f.repKeeper.SeedBondedRole(
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
		addr,
		reptypes.BondedRole{
			Address:            addr,
			RoleType:           reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
			BondStatus:         reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			CurrentBond:        "500000000", // 500 DREAM in udream — covers MinVerifierBond
			TotalCommittedBond: "0",
		},
	)
	return addr
}
