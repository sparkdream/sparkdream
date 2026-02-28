package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	module "sparkdream/x/collect/module"
	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// ---------------------------------------------------------------------------
// Minimal fixture (used by legacy param/genesis tests)
// ---------------------------------------------------------------------------

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
}

func initFixture(t *testing.T) *fixture {
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
		nil, // commonsKeeper
		nil, // forumKeeper
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

// ---------------------------------------------------------------------------
// Mock BankKeeper
// ---------------------------------------------------------------------------

type mockBankKeeper struct {
	spendableCoinsFn               func(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	sendCoinsFn                    func(ctx context.Context, from, to sdk.AccAddress, amt sdk.Coins) error
	sendCoinsFromAccountToModuleFn func(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	sendCoinsFromModuleToAccountFn func(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
	burnCoinsFn                    func(ctx context.Context, moduleName string, amt sdk.Coins) error
}

func (m *mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	if m.spendableCoinsFn != nil {
		return m.spendableCoinsFn(ctx, addr)
	}
	return sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000_000_000)))
}

func (m *mockBankKeeper) SendCoins(ctx context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	if m.sendCoinsFn != nil {
		return m.sendCoinsFn(ctx, from, to, amt)
	}
	return nil
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.sendCoinsFromAccountToModuleFn != nil {
		return m.sendCoinsFromAccountToModuleFn(ctx, sender, recipientModule, amt)
	}
	return nil
}

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error {
	if m.sendCoinsFromModuleToAccountFn != nil {
		return m.sendCoinsFromModuleToAccountFn(ctx, senderModule, recipient, amt)
	}
	return nil
}

func (m *mockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	if m.burnCoinsFn != nil {
		return m.burnCoinsFn(ctx, moduleName, amt)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock RepKeeper
// ---------------------------------------------------------------------------

type mockRepKeeper struct {
	isMemberFn      func(ctx context.Context, addr sdk.AccAddress) bool
	getTrustLevelFn func(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)
	lockDREAMFn     func(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	unlockDREAMFn   func(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	burnDREAMFn     func(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
}

func (m *mockRepKeeper) IsMember(ctx context.Context, addr sdk.AccAddress) bool {
	if m.isMemberFn != nil {
		return m.isMemberFn(ctx, addr)
	}
	return true
}

func (m *mockRepKeeper) GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error) {
	if m.getTrustLevelFn != nil {
		return m.getTrustLevelFn(ctx, addr)
	}
	return reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED, nil
}

func (m *mockRepKeeper) LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if m.lockDREAMFn != nil {
		return m.lockDREAMFn(ctx, addr, amount)
	}
	return nil
}

func (m *mockRepKeeper) UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if m.unlockDREAMFn != nil {
		return m.unlockDREAMFn(ctx, addr, amount)
	}
	return nil
}

func (m *mockRepKeeper) BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if m.burnDREAMFn != nil {
		return m.burnDREAMFn(ctx, addr, amount)
	}
	return nil
}

func (m *mockRepKeeper) GetContentConviction(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (math.LegacyDec, error) {
	return math.LegacyZeroDec(), nil
}

func (m *mockRepKeeper) GetContentStakes(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) ([]reptypes.Stake, error) {
	return nil, nil
}

func (m *mockRepKeeper) CreateAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) (uint64, error) {
	return 1, nil
}

func (m *mockRepKeeper) SlashAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) error {
	return nil
}

func (m *mockRepKeeper) GetAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (reptypes.Stake, error) {
	return reptypes.Stake{}, reptypes.ErrAuthorBondNotFound
}

func (m *mockRepKeeper) ValidateInitiativeReference(ctx context.Context, initiativeID uint64) error {
	return nil
}

func (m *mockRepKeeper) RegisterContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error {
	return nil
}

func (m *mockRepKeeper) RemoveContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error {
	return nil
}

// ---------------------------------------------------------------------------
// Mock CommonsKeeper
// ---------------------------------------------------------------------------

type mockCommonsKeeper struct {
	isCouncilAuthorizedFn func(ctx context.Context, addr string, council string, committee string) bool
}

func (m *mockCommonsKeeper) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if m.isCouncilAuthorizedFn != nil {
		return m.isCouncilAuthorizedFn(ctx, addr, council, committee)
	}
	return true
}

// ---------------------------------------------------------------------------
// Mock ForumKeeper
// ---------------------------------------------------------------------------

type mockForumKeeper struct {
	isSentinelActiveFn      func(ctx context.Context, sentinel string) (bool, error)
	getAvailableBondFn      func(ctx context.Context, sentinel string) (math.Int, error)
	commitBondFn            func(ctx context.Context, sentinel string, amount math.Int, module string, referenceID uint64) error
	releaseBondCommitmentFn func(ctx context.Context, sentinel string, amount math.Int, module string, referenceID uint64) error
	slashBondCommitmentFn   func(ctx context.Context, sentinel string, amount math.Int, module string, referenceID uint64) error
}

func (m *mockForumKeeper) IsSentinelActive(ctx context.Context, sentinel string) (bool, error) {
	if m.isSentinelActiveFn != nil {
		return m.isSentinelActiveFn(ctx, sentinel)
	}
	return true, nil
}

func (m *mockForumKeeper) GetAvailableBond(ctx context.Context, sentinel string) (math.Int, error) {
	if m.getAvailableBondFn != nil {
		return m.getAvailableBondFn(ctx, sentinel)
	}
	return math.NewInt(1000), nil
}

func (m *mockForumKeeper) CommitBond(ctx context.Context, sentinel string, amount math.Int, mod string, referenceID uint64) error {
	if m.commitBondFn != nil {
		return m.commitBondFn(ctx, sentinel, amount, mod, referenceID)
	}
	return nil
}

func (m *mockForumKeeper) ReleaseBondCommitment(ctx context.Context, sentinel string, amount math.Int, mod string, referenceID uint64) error {
	if m.releaseBondCommitmentFn != nil {
		return m.releaseBondCommitmentFn(ctx, sentinel, amount, mod, referenceID)
	}
	return nil
}

func (m *mockForumKeeper) SlashBondCommitment(ctx context.Context, sentinel string, amount math.Int, mod string, referenceID uint64) error {
	if m.slashBondCommitmentFn != nil {
		return m.slashBondCommitmentFn(ctx, sentinel, amount, mod, referenceID)
	}
	return nil
}

func (m *mockForumKeeper) TagExists(ctx context.Context, name string) (bool, error) {
	return true, nil
}

func (m *mockForumKeeper) IsReservedTag(ctx context.Context, name string) (bool, error) {
	return false, nil
}

func (m *mockForumKeeper) HasPost(_ context.Context, _ uint64) bool {
	return true
}

// ---------------------------------------------------------------------------
// Enhanced test fixture (used by message handler / query / endblock tests)
// ---------------------------------------------------------------------------

type testFixture struct {
	ctx           context.Context
	sdkCtx        sdk.Context
	keeper        keeper.Keeper
	msgServer     types.MsgServer
	queryServer   types.QueryServer
	bankKeeper    *mockBankKeeper
	repKeeper     *mockRepKeeper
	commonsKeeper *mockCommonsKeeper
	forumKeeper   *mockForumKeeper
	addressCodec  address.Codec
	authority     string
	owner         string
	ownerAddr     sdk.AccAddress
	member        string
	memberAddr    sdk.AccAddress
	nonMember     string
	nonMemberAddr sdk.AccAddress
	sentinel      string
	sentinelAddr  sdk.AccAddress
}

func initTestFixture(t *testing.T) *testFixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)
	sdkCtx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	bk := &mockBankKeeper{}
	rk := &mockRepKeeper{}
	ck := &mockCommonsKeeper{}
	fk := &mockForumKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		bk,
		ck,
		fk,
	)
	k.SetRepKeeper(rk)

	if err := k.Params.Set(sdkCtx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	authorityStr, _ := addressCodec.BytesToString(authority)

	ownerAddr := sdk.AccAddress([]byte("owner_______________"))
	ownerStr, _ := addressCodec.BytesToString(ownerAddr)

	memberAddr := sdk.AccAddress([]byte("member______________"))
	memberStr, _ := addressCodec.BytesToString(memberAddr)

	nonMemberAddr := sdk.AccAddress([]byte("nonmember___________"))
	nonMemberStr, _ := addressCodec.BytesToString(nonMemberAddr)

	sentinelAddr := sdk.AccAddress([]byte("sentinel____________"))
	sentinelStr, _ := addressCodec.BytesToString(sentinelAddr)

	// Configure repKeeper: owner and member are members; nonMember is not
	rk.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
		return addr.Equals(ownerAddr) || addr.Equals(memberAddr) || addr.Equals(sentinelAddr)
	}

	return &testFixture{
		ctx:           sdkCtx,
		sdkCtx:        sdkCtx,
		keeper:        k,
		msgServer:     keeper.NewMsgServerImpl(k),
		queryServer:   keeper.NewQueryServerImpl(k),
		bankKeeper:    bk,
		repKeeper:     rk,
		commonsKeeper: ck,
		forumKeeper:   fk,
		addressCodec:  addressCodec,
		authority:     authorityStr,
		owner:         ownerStr,
		ownerAddr:     ownerAddr,
		member:        memberStr,
		memberAddr:    memberAddr,
		nonMember:     nonMemberStr,
		nonMemberAddr: nonMemberAddr,
		sentinel:      sentinelStr,
		sentinelAddr:  sentinelAddr,
	}
}

// ---------------------------------------------------------------------------
// Helper methods on testFixture
// ---------------------------------------------------------------------------

type createCollectionOpt func(msg *types.MsgCreateCollection)

func withTTL(expiresAt int64) createCollectionOpt {
	return func(msg *types.MsgCreateCollection) {
		msg.ExpiresAt = expiresAt
	}
}

func withType(ct types.CollectionType) createCollectionOpt {
	return func(msg *types.MsgCreateCollection) {
		msg.Type = ct
	}
}

// createCollection creates a public ACTIVE collection owned by the given creator.
func (f *testFixture) createCollection(t *testing.T, creator string, opts ...createCollectionOpt) uint64 {
	t.Helper()
	msg := &types.MsgCreateCollection{
		Creator:    creator,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "test-collection",
	}
	for _, opt := range opts {
		opt(msg)
	}
	resp, err := f.msgServer.CreateCollection(f.ctx, msg)
	require.NoError(t, err)
	return resp.Id
}

// createTTLCollection creates a TTL collection with expiry, owned by the given creator.
func (f *testFixture) createTTLCollection(t *testing.T, creator string, expiresAt int64) uint64 {
	t.Helper()
	return f.createCollection(t, creator, withTTL(expiresAt))
}

// createPendingCollection creates a PENDING non-member TTL collection.
func (f *testFixture) createPendingCollection(t *testing.T) uint64 {
	t.Helper()
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	blockHeight := sdkCtx.BlockHeight()
	expiresAt := blockHeight + 100000 // well within max_non_member_ttl_blocks

	msg := &types.MsgCreateCollection{
		Creator:    f.nonMember,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "pending-collection",
		ExpiresAt:  expiresAt,
	}
	resp, err := f.msgServer.CreateCollection(f.ctx, msg)
	require.NoError(t, err)
	return resp.Id
}

// addItem adds an item to a collection.
func (f *testFixture) addItem(t *testing.T, collectionID uint64, creator string) uint64 {
	t.Helper()
	resp, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      creator,
		CollectionId: collectionID,
		Title:        "test-item",
	})
	require.NoError(t, err)
	return resp.Id
}

// registerCurator registers a curator with the given bond amount.
func (f *testFixture) registerCurator(t *testing.T, creator string, bond int64) {
	t.Helper()
	_, err := f.msgServer.RegisterCurator(f.ctx, &types.MsgRegisterCurator{
		Creator:    creator,
		BondAmount: math.NewInt(bond),
	})
	require.NoError(t, err)
}

// addCollaborator adds a collaborator to a collection.
func (f *testFixture) addCollaborator(t *testing.T, collectionID uint64, creator, collab string, role types.CollaboratorRole) {
	t.Helper()
	_, err := f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
		Creator:      creator,
		CollectionId: collectionID,
		Address:      collab,
		Role:         role,
	})
	require.NoError(t, err)
}

// advanceBlockHeight increments the block height on the fixture context.
func (f *testFixture) advanceBlockHeight(delta int64) {
	f.sdkCtx = f.sdkCtx.WithBlockHeight(f.sdkCtx.BlockHeight() + delta)
	f.ctx = f.sdkCtx
}

// setBlockHeight sets the absolute block height.
func (f *testFixture) setBlockHeight(height int64) {
	f.sdkCtx = f.sdkCtx.WithBlockHeight(height)
	f.ctx = f.sdkCtx
}
