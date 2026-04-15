package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/stretchr/testify/require"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"
	ibctypes "github.com/cosmos/ibc-go/v10/modules/core/types"

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
	k.SetRepKeeper(&mockRepKeeper{})

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

type mockCommonsKeeper struct{}

func (m *mockCommonsKeeper) IsCouncilAuthorized(_ context.Context, addr string, _, _ string) bool {
	// In tests, authority is always authorized
	return true
}

type mockRepKeeper struct{}

func (m *mockRepKeeper) GetTrustLevel(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
	return reptypes.TrustLevel(3), nil // TRUSTED
}

func (m *mockRepKeeper) BurnDREAM(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
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
	_, err := ms.RegisterBridge(f.ctx, &types.MsgRegisterBridge{
		Authority: f.authority,
		Operator:  operatorStr,
		PeerId:    peerID,
		Protocol:  "activitypub",
		Endpoint:  "https://" + operatorSeed + ".example.com",
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

// bondTestVerifier creates and bonds a verifier, returns address string.
func bondTestVerifier(t *testing.T, f *fixture, ms types.MsgServer, seed string) string {
	t.Helper()
	addr := testAddr(t, f, seed)
	_, err := ms.BondVerifier(f.ctx, &types.MsgBondVerifier{
		Creator: addr,
		Amount:  math.NewInt(500),
	})
	require.NoError(t, err)
	return addr
}
