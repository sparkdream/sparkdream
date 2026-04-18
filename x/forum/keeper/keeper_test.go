package keeper_test

import (
	"bytes"
	"context"
	"fmt"
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

	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/forum/keeper"
	module "sparkdream/x/forum/module"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// Test addresses - generated dynamically with valid checksums
var (
	testCreatorAddr   sdk.AccAddress
	testCreator       string
	testCreator2Addr  sdk.AccAddress
	testCreator2      string
	testSentinelAddr  sdk.AccAddress
	testSentinel      string
	testAuthorityAddr sdk.AccAddress
	testAuthority     string
	testAddrCodec     address.Codec
)

func init() {
	// Initialize the address codec for cosmos prefix
	testAddrCodec = addresscodec.NewBech32Codec("cosmos")

	// Create deterministic test addresses with valid checksums
	// Each address uses a different byte pattern to ensure uniqueness
	testCreatorAddr = sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	testCreator, _ = testAddrCodec.BytesToString(testCreatorAddr)

	testCreator2Addr = sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	testCreator2, _ = testAddrCodec.BytesToString(testCreator2Addr)

	testSentinelAddr = sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	testSentinel, _ = testAddrCodec.BytesToString(testSentinelAddr)

	testAuthorityAddr = sdk.AccAddress(bytes.Repeat([]byte{4}, 20))
	testAuthority, _ = testAddrCodec.BytesToString(testAuthorityAddr)
}

type fixture struct {
	ctx           context.Context
	keeper        keeper.Keeper
	addressCodec  address.Codec
	msgServer     types.MsgServer
	bankKeeper    *mockBankKeeper
	repKeeper     *mockRepKeeper
	commonsKeeper *mockCommonsKeeper
}

// mockBankKeeper implements types.BankKeeper for testing
type mockBankKeeper struct {
	SpendableCoinsFn               func(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFn                    func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModuleFn func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccountFn func(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModuleFn  func(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	BurnCoinsFn                    func(ctx context.Context, moduleName string, amt sdk.Coins) error
	MintCoinsFn                    func(ctx context.Context, moduleName string, amt sdk.Coins) error
	// Track calls for assertion
	SendCoinsFromAccountToModuleCalls []forumSendCoinsCall
	SendCoinsFromModuleToModuleCalls  []forumModToModCall
	BurnCoinsCalls                    []forumBurnCoinsCall
}

type forumModToModCall struct {
	SenderModule    string
	RecipientModule string
	Amt             sdk.Coins
}

type forumSendCoinsCall struct {
	SenderAddr      sdk.AccAddress
	RecipientModule string
	Amt             sdk.Coins
}

type forumBurnCoinsCall struct {
	ModuleName string
	Amt        sdk.Coins
}

func (m *mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	if m.SpendableCoinsFn != nil {
		return m.SpendableCoinsFn(ctx, addr)
	}
	return sdk.NewCoins(sdk.NewCoin("usprkdrm", math.NewInt(1000000000)))
}

func (m *mockBankKeeper) SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.SendCoinsFn != nil {
		return m.SendCoinsFn(ctx, fromAddr, toAddr, amt)
	}
	return nil
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	m.SendCoinsFromAccountToModuleCalls = append(m.SendCoinsFromAccountToModuleCalls, forumSendCoinsCall{
		SenderAddr:      senderAddr,
		RecipientModule: recipientModule,
		Amt:             amt,
	})
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
	m.SendCoinsFromModuleToModuleCalls = append(m.SendCoinsFromModuleToModuleCalls, forumModToModCall{
		SenderModule:    senderModule,
		RecipientModule: recipientModule,
		Amt:             amt,
	})
	if m.SendCoinsFromModuleToModuleFn != nil {
		return m.SendCoinsFromModuleToModuleFn(ctx, senderModule, recipientModule, amt)
	}
	return nil
}

func (m *mockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	m.BurnCoinsCalls = append(m.BurnCoinsCalls, forumBurnCoinsCall{
		ModuleName: moduleName,
		Amt:        amt,
	})
	if m.BurnCoinsFn != nil {
		return m.BurnCoinsFn(ctx, moduleName, amt)
	}
	return nil
}

func (m *mockBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	if m.MintCoinsFn != nil {
		return m.MintCoinsFn(ctx, moduleName, amt)
	}
	return nil
}

// mockRepKeeper implements types.RepKeeper for testing
type mockRepKeeper struct {
	CreateAppealInitiativeFn        func(ctx context.Context, initiativeType string, payload []byte, deadline int64) (uint64, error)
	IsMemberFn                      func(ctx context.Context, addr sdk.AccAddress) bool
	ValidateInitiativeReferenceFn   func(ctx context.Context, initiativeID uint64) error
	RegisterContentInitiativeLinkFn func(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error
	RemoveContentInitiativeLinkFn   func(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error
	nextInitiativeID                uint64
	tags                            map[string]reptypes.Tag
	reservedTags                    map[string]reptypes.ReservedTag
	sentinels                       map[string]reptypes.SentinelActivity
}

func (m *mockRepKeeper) TagExists(_ context.Context, name string) (bool, error) {
	_, ok := m.tags[name]
	return ok, nil
}

func (m *mockRepKeeper) IsReservedTag(_ context.Context, name string) (bool, error) {
	_, ok := m.reservedTags[name]
	return ok, nil
}

func (m *mockRepKeeper) GetTag(_ context.Context, name string) (reptypes.Tag, error) {
	t, ok := m.tags[name]
	if !ok {
		return reptypes.Tag{}, reptypes.ErrTagNotRegistered
	}
	return t, nil
}

func (m *mockRepKeeper) IncrementTagUsage(_ context.Context, name string, ts int64) error {
	t, ok := m.tags[name]
	if !ok {
		return reptypes.ErrTagNotRegistered
	}
	t.UsageCount++
	t.LastUsedAt = ts
	if m.tags == nil {
		m.tags = make(map[string]reptypes.Tag)
	}
	m.tags[name] = t
	return nil
}

func (m *mockRepKeeper) SetReservedTag(_ context.Context, rt reptypes.ReservedTag) error {
	if m.reservedTags == nil {
		m.reservedTags = make(map[string]reptypes.ReservedTag)
	}
	m.reservedTags[rt.Name] = rt
	return nil
}

func (m *mockRepKeeper) MintDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	return nil
}

func (m *mockRepKeeper) BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	return nil
}

func (m *mockRepKeeper) LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	return nil
}

func (m *mockRepKeeper) UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	return nil
}

func (m *mockRepKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress) (math.Int, error) {
	return math.NewInt(1000000), nil
}

func (m *mockRepKeeper) TransferDREAM(ctx context.Context, sender, recipient sdk.AccAddress, amount math.Int, purpose reptypes.TransferPurpose) error {
	return nil
}

func (m *mockRepKeeper) IsMember(ctx context.Context, addr sdk.AccAddress) bool {
	if m.IsMemberFn != nil {
		return m.IsMemberFn(ctx, addr)
	}
	return true
}

func (m *mockRepKeeper) IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool {
	return true
}

func (m *mockRepKeeper) GetMember(ctx context.Context, addr sdk.AccAddress) (reptypes.Member, error) {
	// Return a member with sufficient StakedDream for sentinel operations
	staked := math.NewInt(5000)
	return reptypes.Member{
		StakedDream: &staked,
		TrustLevel:  reptypes.TrustLevel_TRUST_LEVEL_TRUSTED,
	}, nil
}

func (m *mockRepKeeper) GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error) {
	return reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED, nil
}

func (m *mockRepKeeper) GetReputationTier(ctx context.Context, addr sdk.AccAddress) (uint64, error) {
	return 5, nil // Return high tier to allow sentinel operations
}

func (m *mockRepKeeper) ZeroMember(ctx context.Context, memberAddr sdk.AccAddress, reason string) error {
	return nil
}

func (m *mockRepKeeper) DemoteMember(ctx context.Context, memberAddr sdk.AccAddress, reason string) error {
	return nil
}

func (m *mockRepKeeper) CreateAppealInitiative(ctx context.Context, initiativeType string, payload []byte, deadline int64) (uint64, error) {
	if m.CreateAppealInitiativeFn != nil {
		return m.CreateAppealInitiativeFn(ctx, initiativeType, payload, deadline)
	}
	m.nextInitiativeID++
	return m.nextInitiativeID, nil
}

func (m *mockRepKeeper) GetContentConviction(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (math.LegacyDec, error) {
	return math.LegacyZeroDec(), nil
}

func (m *mockRepKeeper) CreateAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) (uint64, error) {
	return 1, nil
}

func (m *mockRepKeeper) SlashAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) error {
	return nil
}

func (m *mockRepKeeper) ValidateInitiativeReference(ctx context.Context, initiativeID uint64) error {
	if m.ValidateInitiativeReferenceFn != nil {
		return m.ValidateInitiativeReferenceFn(ctx, initiativeID)
	}
	return nil
}

func (m *mockRepKeeper) RegisterContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error {
	if m.RegisterContentInitiativeLinkFn != nil {
		return m.RegisterContentInitiativeLinkFn(ctx, initiativeID, targetType, targetID)
	}
	return nil
}

func (m *mockRepKeeper) RemoveContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error {
	if m.RemoveContentInitiativeLinkFn != nil {
		return m.RemoveContentInitiativeLinkFn(ctx, initiativeID, targetType, targetID)
	}
	return nil
}

func (m *mockRepKeeper) GetSentinel(_ context.Context, addr string) (reptypes.SentinelActivity, error) {
	sa, ok := m.sentinels[addr]
	if !ok {
		return reptypes.SentinelActivity{}, reptypes.ErrSentinelNotFound
	}
	return sa, nil
}

func (m *mockRepKeeper) ReserveBond(_ context.Context, addr string, amount math.Int) error {
	sa, ok := m.sentinels[addr]
	if !ok {
		return reptypes.ErrSentinelNotFound
	}
	current, _ := math.NewIntFromString(sa.CurrentBond)
	committed, _ := math.NewIntFromString(sa.TotalCommittedBond)
	avail := current.Sub(committed)
	if avail.LT(amount) {
		return reptypes.ErrInsufficientSentinelBond
	}
	sa.TotalCommittedBond = committed.Add(amount).String()
	m.sentinels[addr] = sa
	return nil
}

func (m *mockRepKeeper) RecordActivity(_ context.Context, addr string) error {
	sa, ok := m.sentinels[addr]
	if !ok {
		return nil
	}
	sa.ConsecutiveInactiveEpochs = 0
	m.sentinels[addr] = sa
	return nil
}

func (m *mockRepKeeper) SetBondStatus(_ context.Context, addr string, status reptypes.SentinelBondStatus, cooldownUntil int64) error {
	sa, ok := m.sentinels[addr]
	if !ok {
		return fmt.Errorf("sentinel %s not found", addr)
	}
	sa.BondStatus = status
	sa.DemotionCooldownUntil = cooldownUntil
	m.sentinels[addr] = sa
	return nil
}

func (m *mockRepKeeper) GetSalvationCounters(_ context.Context, _ string) (uint32, int64, error) {
	return 0, 0, nil
}

func (m *mockRepKeeper) UpdateSalvationCounters(_ context.Context, _ string, _ uint32, _ int64) error {
	return nil
}

// mockCommonsKeeper implements types.CommonsKeeper for testing.
type mockCommonsKeeper struct {
	IsGroupPolicyMemberFn  func(ctx context.Context, policyAddr string, memberAddr string) (bool, error)
	IsGroupPolicyAddressFn func(ctx context.Context, addr string) bool
	IsCouncilAuthorizedFn  func(ctx context.Context, addr string, council string, committee string) bool
	categories             map[uint64]commonstypes.Category
	categorySeq            uint64
}

func (m *mockCommonsKeeper) setCategory(cat commonstypes.Category) {
	if m.categories == nil {
		m.categories = make(map[uint64]commonstypes.Category)
	}
	m.categories[cat.CategoryId] = cat
}

func (m *mockCommonsKeeper) GetCategory(_ context.Context, id uint64) (commonstypes.Category, bool) {
	cat, ok := m.categories[id]
	return cat, ok
}

func (m *mockCommonsKeeper) HasCategory(_ context.Context, id uint64) bool {
	_, ok := m.categories[id]
	return ok
}

func (m *mockCommonsKeeper) IsGroupPolicyMember(ctx context.Context, policyAddr string, memberAddr string) (bool, error) {
	if m.IsGroupPolicyMemberFn != nil {
		return m.IsGroupPolicyMemberFn(ctx, policyAddr, memberAddr)
	}
	return false, nil
}

func (m *mockCommonsKeeper) IsGroupPolicyAddress(ctx context.Context, addr string) bool {
	if m.IsGroupPolicyAddressFn != nil {
		return m.IsGroupPolicyAddressFn(ctx, addr)
	}
	return false
}

func (m *mockCommonsKeeper) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if m.IsCouncilAuthorizedFn != nil {
		return m.IsCouncilAuthorizedFn(ctx, addr, council, committee)
	}
	return false
}

func initFixtureWithCommons(t *testing.T, commonsKeeper types.CommonsKeeper) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	bankKeeper := &mockBankKeeper{}
	repKeeper := &mockRepKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority.Bytes(),
		bankKeeper,
		repKeeper,
		commonsKeeper,
	)

	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	_, _ = k.PostSeq.Next(ctx)
	_, _ = k.BountySeq.Next(ctx)

	ck, _ := commonsKeeper.(*mockCommonsKeeper)

	return &fixture{
		ctx:           ctx,
		keeper:        k,
		addressCodec:  addressCodec,
		msgServer:     keeper.NewMsgServerImpl(k),
		bankKeeper:    bankKeeper,
		repKeeper:     repKeeper,
		commonsKeeper: ck,
	}
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	bankKeeper := &mockBankKeeper{}
	repKeeper := &mockRepKeeper{}
	// Only the gov authority is council-authorized; group membership and
	// policy checks are permissive by default.
	commonsKeeper := &mockCommonsKeeper{}
	commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, addr string, _ string, _ string) bool {
		addrBytes, err := addressCodec.StringToBytes(addr)
		if err != nil {
			return false
		}
		return bytes.Equal(authority.Bytes(), addrBytes)
	}
	commonsKeeper.IsGroupPolicyMemberFn = func(_ context.Context, _ string, _ string) (bool, error) {
		return true, nil
	}
	commonsKeeper.IsGroupPolicyAddressFn = func(_ context.Context, _ string) bool {
		return true
	}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority.Bytes(),
		bankKeeper,
		repKeeper,
		commonsKeeper,
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	// Prime sequences to start at 1 (skip 0 to avoid confusion with zero-value)
	// PostId=0 would conflict with ParentId=0 meaning "no parent"
	_, _ = k.PostSeq.Next(ctx)
	_, _ = k.BountySeq.Next(ctx)

	return &fixture{
		ctx:           ctx,
		keeper:        k,
		addressCodec:  addressCodec,
		msgServer:     keeper.NewMsgServerImpl(k),
		bankKeeper:    bankKeeper,
		repKeeper:    repKeeper,
		commonsKeeper: commonsKeeper,
	}
}

// Helper to get SDK context from fixture
func (f *fixture) sdkCtx() sdk.Context {
	return sdk.UnwrapSDKContext(f.ctx)
}

// Helper to create a test post
func (f *fixture) createTestPost(t *testing.T, author string, parentId, categoryId uint64) types.Post {
	t.Helper()
	postID, err := f.keeper.PostSeq.Next(f.ctx)
	if err != nil {
		t.Fatalf("failed to get next post ID: %v", err)
	}

	rootId := postID
	if parentId != 0 {
		parent, err := f.keeper.Post.Get(f.ctx, parentId)
		if err == nil {
			rootId = parent.RootId
		}
	}

	post := types.Post{
		PostId:     postID,
		Author:     author,
		CategoryId: categoryId,
		ParentId:   parentId,
		RootId:     rootId,
		Content:    "Test content",
		Status:     types.PostStatus_POST_STATUS_ACTIVE,
		CreatedAt:  f.sdkCtx().BlockTime().Unix(),
	}

	if err := f.keeper.Post.Set(f.ctx, postID, post); err != nil {
		t.Fatalf("failed to create test post: %v", err)
	}

	return post
}

func (f *fixture) createTestCategory(t *testing.T, title string) commonstypes.Category {
	t.Helper()
	if f.commonsKeeper == nil {
		t.Fatalf("fixture has no mockCommonsKeeper")
	}
	f.commonsKeeper.categorySeq++
	cat := commonstypes.Category{
		CategoryId:  f.commonsKeeper.categorySeq,
		Title:       title,
		Description: "Test category",
	}
	f.commonsKeeper.setCategory(cat)
	return cat
}

// Helper to create a test bounty
func (f *fixture) createTestBounty(t *testing.T, creator string, threadId uint64, amount string) types.Bounty {
	t.Helper()
	bountyID, err := f.keeper.BountySeq.Next(f.ctx)
	if err != nil {
		t.Fatalf("failed to get next bounty ID: %v", err)
	}

	bounty := types.Bounty{
		Id:        bountyID,
		Creator:   creator,
		ThreadId:  threadId,
		Amount:    amount,
		Status:    types.BountyStatus_BOUNTY_STATUS_ACTIVE,
		ExpiresAt: f.sdkCtx().BlockTime().Unix() + 86400*7, // 7 days
		CreatedAt: f.sdkCtx().BlockTime().Unix(),
	}

	if err := f.keeper.Bounty.Set(f.ctx, bountyID, bounty); err != nil {
		t.Fatalf("failed to create test bounty: %v", err)
	}

	return bounty
}

// Helper to create a test sentinel. Registers both the forum-local counter
// record and (via the mock rep keeper) the accountability/bond record owned
// by x/rep.
func (f *fixture) createTestSentinel(t *testing.T, addr string, bond string) types.SentinelActivity {
	t.Helper()
	sentinel := types.SentinelActivity{
		Address:    addr,
		TotalHides: 0,
		EpochHides: 0,
		TotalLocks: 0,
		EpochLocks: 0,
		TotalMoves: 0,
		EpochMoves: 0,
	}

	if err := f.keeper.SentinelActivity.Set(f.ctx, addr, sentinel); err != nil {
		t.Fatalf("failed to create test sentinel: %v", err)
	}

	if f.repKeeper.sentinels == nil {
		f.repKeeper.sentinels = make(map[string]reptypes.SentinelActivity)
	}
	f.repKeeper.sentinels[addr] = reptypes.SentinelActivity{
		Address:            addr,
		CurrentBond:        bond,
		TotalCommittedBond: "0",
		BondStatus:         reptypes.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
	}

	return sentinel
}

// Helper to create a test tag in the mock rep keeper's tag registry.
func (f *fixture) createTestTag(t *testing.T, name string) reptypes.Tag {
	t.Helper()
	tag := reptypes.Tag{
		Name:      name,
		CreatedAt: f.sdkCtx().BlockTime().Unix(),
	}
	if f.repKeeper.tags == nil {
		f.repKeeper.tags = make(map[string]reptypes.Tag)
	}
	f.repKeeper.tags[name] = tag
	return tag
}

