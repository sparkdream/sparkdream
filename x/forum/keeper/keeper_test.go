package keeper_test

import (
	"bytes"
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

	"sparkdream/x/forum/keeper"
	module "sparkdream/x/forum/module"
	"sparkdream/x/forum/types"
)

// Test addresses - generated dynamically with valid checksums
var (
	testCreatorAddr  sdk.AccAddress
	testCreator      string
	testCreator2Addr sdk.AccAddress
	testCreator2     string
	testSentinelAddr sdk.AccAddress
	testSentinel     string
	testAuthorityAddr sdk.AccAddress
	testAuthority    string
	testAddrCodec    address.Codec
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
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	msgServer    types.MsgServer
	bankKeeper   *mockBankKeeper
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

func (m *mockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
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

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("cosmos")
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
		nil, // repKeeper - nil uses fallback stubs for testing
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	// Prime sequences to start at 1 (skip 0 to avoid confusion with zero-value)
	// PostId=0 would conflict with ParentId=0 meaning "no parent"
	_, _ = k.PostSeq.Next(ctx)
	_, _ = k.CategorySeq.Next(ctx)
	_, _ = k.BountySeq.Next(ctx)
	_, _ = k.TagBudgetSeq.Next(ctx)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		msgServer:    keeper.NewMsgServerImpl(k),
		bankKeeper:   bankKeeper,
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

// Helper to create a test category
func (f *fixture) createTestCategory(t *testing.T, title string) types.Category {
	t.Helper()
	catID, err := f.keeper.CategorySeq.Next(f.ctx)
	if err != nil {
		t.Fatalf("failed to get next category ID: %v", err)
	}

	cat := types.Category{
		CategoryId:  catID,
		Title:       title,
		Description: "Test category",
	}

	if err := f.keeper.Category.Set(f.ctx, catID, cat); err != nil {
		t.Fatalf("failed to create test category: %v", err)
	}

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

// Helper to create a test sentinel
func (f *fixture) createTestSentinel(t *testing.T, addr string, bond string) types.SentinelActivity {
	t.Helper()
	sentinel := types.SentinelActivity{
		Address:            addr,
		CurrentBond:        bond,
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
		TotalHides:         0,
		EpochHides:         0,
		TotalLocks:         0,
		EpochLocks:         0,
		TotalMoves:         0,
		EpochMoves:         0,
	}

	if err := f.keeper.SentinelActivity.Set(f.ctx, addr, sentinel); err != nil {
		t.Fatalf("failed to create test sentinel: %v", err)
	}

	return sentinel
}

// Helper to create a test tag
func (f *fixture) createTestTag(t *testing.T, name string) types.Tag {
	t.Helper()
	tag := types.Tag{
		Name:      name,
		CreatedAt: f.sdkCtx().BlockTime().Unix(),
	}

	if err := f.keeper.Tag.Set(f.ctx, name, tag); err != nil {
		t.Fatalf("failed to create test tag: %v", err)
	}

	return tag
}

// Helper to create a test tag budget
func (f *fixture) createTestTagBudget(t *testing.T, groupAccount, tag, balance string) types.TagBudget {
	t.Helper()
	budgetID, err := f.keeper.TagBudgetSeq.Next(f.ctx)
	if err != nil {
		t.Fatalf("failed to get next tag budget ID: %v", err)
	}

	budget := types.TagBudget{
		Id:           budgetID,
		GroupAccount: groupAccount,
		Tag:          tag,
		PoolBalance:  balance,
		Active:       true,
		CreatedAt:    f.sdkCtx().BlockTime().Unix(),
	}

	if err := f.keeper.TagBudget.Set(f.ctx, budgetID, budget); err != nil {
		t.Fatalf("failed to create test tag budget: %v", err)
	}

	return budget
}
