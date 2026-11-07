package keeper_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/core/address"
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
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
	reptypes "sparkdream/x/rep/types"
)

// --- Test Fixture ---

type fixture struct {
	ctx          sdk.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	mockBank     *MockBankKeeper
	mockCommons  *MockCommonsKeeper
	mockRep      *MockRepKeeper
	councilAddr  string
}

func initFixture(t *testing.T) *fixture {
	storeKey := sdkstore.NewKVStoreKey(types.StoreKey)
	memStoreKey := sdkstore.NewMemoryStoreKey("mem_name")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, sdkstore.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdkstore.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	// Use specific SDK context for BlockTime/Mocking
	ctx := sdk.NewContext(stateStore, cmtproto.Header{}, false, log.NewNopLogger())
	ctx = ctx.WithBlockTime(time.Now())

	cdc := codectestutil.CodecOptions{}.NewCodec()
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	authority := sdk.AccAddress([]byte("authority"))

	// Create a mock council address
	councilAddr := sdk.AccAddress([]byte("council_policy_addr_"))
	councilAddrStr := councilAddr.String()

	// Inject Mocks
	mockBK := NewMockBankKeeper()
	mockCK := NewMockCommonsKeeper()
	mockRK := NewMockRepKeeper()

	storeService := runtime.NewKVStoreService(storeKey)

	k := keeper.NewKeeper(
		storeService,
		cdc,
		addressCodec,
		authority,
		mockBK, // BankKeeper
		mockCK, // CommonsKeeper
		mockRK, // RepKeeper
	)
	k.SetIdentityKeeper(&mockIdentityKeeper{})

	// Initialize Params
	err := k.Params.Set(ctx, types.DefaultParams())
	require.NoError(t, err)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		mockBank:     mockBK,
		mockCommons:  mockCK,
		mockRep:      mockRK,
		councilAddr:  councilAddrStr,
	}
}

// --- Shared Mocks ---

// MockCommonsKeeper (Used in dispute tests)
// mockIdentityKeeper returns legacy denoms for unit tests.
type mockIdentityKeeper struct{}

func (mockIdentityKeeper) IsIdentityKeeper()                     {}
func (m *mockIdentityKeeper) BondDenom(_ context.Context) string { return "uspark" }

type MockCommonsKeeper struct {
	Groups      map[string]commonstypes.Group
	PolicyPerms map[string]commonstypes.PolicyPermissions
	Members     map[string]bool // "councilName|address" -> isMember
	getError    error
}

func NewMockCommonsKeeper() *MockCommonsKeeper {
	return &MockCommonsKeeper{
		Groups:      make(map[string]commonstypes.Group),
		PolicyPerms: make(map[string]commonstypes.PolicyPermissions),
		Members:     make(map[string]bool),
	}
}

func (m *MockCommonsKeeper) Reset() {
	m.Groups = make(map[string]commonstypes.Group)
	m.PolicyPerms = make(map[string]commonstypes.PolicyPermissions)
	m.Members = make(map[string]bool)
	m.getError = nil
}

func (m *MockCommonsKeeper) GetGroup(ctx context.Context, name string) (commonstypes.Group, error) {
	if m.getError != nil {
		return commonstypes.Group{}, m.getError
	}

	if group, found := m.Groups[name]; found {
		return group, nil
	}
	return commonstypes.Group{}, sdkerrors.ErrInvalidRequest.Wrap("group not found")
}

func (m *MockCommonsKeeper) SetGroup(ctx context.Context, name string, group commonstypes.Group) error {
	m.Groups[name] = group
	return nil
}

func (m *MockCommonsKeeper) GetPolicyPermissions(ctx context.Context, policyAddress string) (commonstypes.PolicyPermissions, error) {
	if perms, found := m.PolicyPerms[policyAddress]; found {
		return perms, nil
	}
	return commonstypes.PolicyPermissions{}, nil
}

func (m *MockCommonsKeeper) SetPolicyPermissions(ctx context.Context, policyAddress string, perms commonstypes.PolicyPermissions) error {
	m.PolicyPerms[policyAddress] = perms
	return nil
}

func (m *MockCommonsKeeper) IsCouncilAuthorized(_ context.Context, _ string, _ string, _ string) bool {
	return false
}

// IsCouncilOrCommitteePolicy mirrors the commons helper for the Commons
// Council and its Operations Committee: the policy addresses only.
func (m *MockCommonsKeeper) IsCouncilOrCommitteePolicy(_ context.Context, addr string, _ string, _ string) bool {
	for _, name := range []string{"Commons Council", "Commons Operations Committee"} {
		if g, ok := m.Groups[name]; ok && g.PolicyAddress != "" && g.PolicyAddress == addr {
			return true
		}
	}
	return false
}

func (m *MockCommonsKeeper) HasMember(_ context.Context, councilName string, address string) (bool, error) {
	key := councilName + "|" + address
	return m.Members[key], nil
}

func (m *MockCommonsKeeper) AddMember(_ context.Context, councilName string, member commonstypes.Member) error {
	key := councilName + "|" + member.Address
	m.Members[key] = true
	return nil
}

// MockBankKeeper
type MockBankKeeper struct {
	SentCoins map[string]sdk.Coins
	// ToModule and Burned track registration fees sent to and burned by a
	// module account.
	ToModule map[string]sdk.Coins
	Burned   map[string]sdk.Coins
	sendErr  error
	hasFunds map[string]math.Int
}

func NewMockBankKeeper() *MockBankKeeper {
	return &MockBankKeeper{
		SentCoins: make(map[string]sdk.Coins),
		ToModule:  make(map[string]sdk.Coins),
		Burned:    make(map[string]sdk.Coins),
		hasFunds:  make(map[string]math.Int),
	}
}

func (m *MockBankKeeper) Reset() {
	m.SentCoins = make(map[string]sdk.Coins)
	m.ToModule = make(map[string]sdk.Coins)
	m.Burned = make(map[string]sdk.Coins)
	m.hasFunds = make(map[string]math.Int)
	m.sendErr = nil
}

func (m *MockBankKeeper) SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	key := fromAddr.String() + "|" + toAddr.String()
	current := m.SentCoins[key]
	m.SentCoins[key] = current.Add(amt...)
	return nil
}

func (m *MockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.ToModule[recipientModule] = m.ToModule[recipientModule].Add(amt...)
	return nil
}

func (m *MockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	held := m.ToModule[moduleName]
	if !held.IsAllGTE(amt) {
		return fmt.Errorf("module %s holds %s, cannot burn %s", moduleName, held, amt)
	}
	m.ToModule[moduleName] = held.Sub(amt...)
	m.Burned[moduleName] = m.Burned[moduleName].Add(amt...)
	return nil
}

func (m *MockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	if amount, ok := m.hasFunds[addr.String()]; ok {
		return sdk.NewCoins(sdk.NewCoin("uspark", amount))
	}
	return sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000000000)))
}

// MockRepKeeper implements the RepKeeper interface for DREAM token operations
// and the IsActiveMember gate used to authorize name registration.
type MockRepKeeper struct {
	LockedDREAM   map[string]math.Int // addr -> total locked
	UnlockedDREAM map[string]math.Int // addr -> total unlocked
	BurnedDREAM   map[string]math.Int // addr -> total burned
	ActiveMembers map[string]bool     // bech32 -> active
	lockErr       error
	unlockErr     error
	burnErr       error
	// unlockFailures fails the next N UnlockDREAM calls.
	unlockFailures int
	// Gone holds zeroed addresses: their locks no longer exist, so an
	// unlock fails with ErrInsufficientStake, as in x/rep.
	Gone map[string]bool
	// BurnFailFor makes BurnDREAM fail for these addresses only.
	BurnFailFor map[string]bool
}

func NewMockRepKeeper() *MockRepKeeper {
	return &MockRepKeeper{
		LockedDREAM:   make(map[string]math.Int),
		UnlockedDREAM: make(map[string]math.Int),
		BurnedDREAM:   make(map[string]math.Int),
		ActiveMembers: make(map[string]bool),
	}
}

func (m *MockRepKeeper) Reset() {
	m.LockedDREAM = make(map[string]math.Int)
	m.UnlockedDREAM = make(map[string]math.Int)
	m.BurnedDREAM = make(map[string]math.Int)
	m.ActiveMembers = make(map[string]bool)
	m.lockErr = nil
	m.unlockErr = nil
	m.burnErr = nil
	m.Gone = nil
}

// SetActiveMember marks an address as an active x/rep member for tests.
func (m *MockRepKeeper) SetActiveMember(addr string) {
	m.ActiveMembers[addr] = true
}

// Alias contract defaults: no migrations in unit tests.
func (m *MockRepKeeper) CanActAs(_ context.Context, signer, stored string) bool {
	return signer == stored
}
func (m *MockRepKeeper) SameMember(_ context.Context, a, b string) bool { return a == b }
func (m *MockRepKeeper) AllAddressesOf(_ context.Context, member string) []string {
	return []string{member}
}

func (m *MockRepKeeper) IsActiveMember(_ context.Context, addr sdk.AccAddress) bool {
	return m.ActiveMembers[addr.String()]
}

func (m *MockRepKeeper) LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if m.lockErr != nil {
		return m.lockErr
	}
	key := addr.String()
	current, ok := m.LockedDREAM[key]
	if !ok {
		current = math.ZeroInt()
	}
	m.LockedDREAM[key] = current.Add(amount)
	return nil
}

func (m *MockRepKeeper) UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if m.unlockErr != nil {
		return m.unlockErr
	}
	if m.unlockFailures > 0 {
		m.unlockFailures--
		return fmt.Errorf("unlock failed")
	}
	if m.Gone[addr.String()] {
		return reptypes.ErrInsufficientStake
	}
	key := addr.String()
	current, ok := m.UnlockedDREAM[key]
	if !ok {
		current = math.ZeroInt()
	}
	m.UnlockedDREAM[key] = current.Add(amount)
	return nil
}

// BurnLockedDREAM mirrors x/rep: the lock is released and the same amount
// burned, all or nothing.
func (m *MockRepKeeper) BurnLockedDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if err := m.UnlockDREAM(ctx, addr, amount); err != nil {
		return err
	}
	return m.BurnDREAM(ctx, addr, amount)
}

func (m *MockRepKeeper) BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if m.burnErr != nil {
		return m.burnErr
	}
	key := addr.String()
	if m.BurnFailFor[key] {
		return fmt.Errorf("burn refused for %s", key)
	}
	current, ok := m.BurnedDREAM[key]
	if !ok {
		current = math.ZeroInt()
	}
	m.BurnedDREAM[key] = current.Add(amount)
	return nil
}
