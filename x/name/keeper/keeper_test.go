package keeper_test

import (
	"context"
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
	"github.com/cosmos/cosmos-sdk/x/group"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// --- Test Fixture ---

type fixture struct {
	ctx          sdk.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	mockBank     *MockBankKeeper
	mockCommons  *MockCommonsKeeper
	mockGroup    *MockGroupKeeperReg
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
	mockGK := &MockGroupKeeperReg{members: make(map[string]bool)}
	mockCK := NewMockCommonsKeeper()

	storeService := runtime.NewKVStoreService(storeKey)

	k := keeper.NewKeeper(
		storeService,
		cdc,
		addressCodec,
		authority,
		mockBK, // BankKeeper
		mockCK, // CommonsKeeper
		mockGK, // GroupKeeper
	)

	// Initialize Params
	err := k.Params.Set(ctx, types.DefaultParams())
	require.NoError(t, err)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		mockBank:     mockBK,
		mockCommons:  mockCK,
		mockGroup:    mockGK,
		councilAddr:  councilAddrStr,
	}
}

// --- Shared Mocks ---

// MockCommonsKeeper (Used in dispute tests)
type MockCommonsKeeper struct {
	ExtendedGroups map[string]commonstypes.ExtendedGroup
	PolicyPerms    map[string]commonstypes.PolicyPermissions
	getError       error
}

func NewMockCommonsKeeper() *MockCommonsKeeper {
	return &MockCommonsKeeper{
		ExtendedGroups: make(map[string]commonstypes.ExtendedGroup),
		PolicyPerms:    make(map[string]commonstypes.PolicyPermissions),
	}
}

func (m *MockCommonsKeeper) Reset() {
	m.ExtendedGroups = make(map[string]commonstypes.ExtendedGroup)
	m.PolicyPerms = make(map[string]commonstypes.PolicyPermissions)
	m.getError = nil
}

func (m *MockCommonsKeeper) GetExtendedGroup(ctx context.Context, name string) (commonstypes.ExtendedGroup, error) {
	if m.getError != nil {
		return commonstypes.ExtendedGroup{}, m.getError
	}

	if group, found := m.ExtendedGroups[name]; found {
		return group, nil
	}
	return commonstypes.ExtendedGroup{}, sdkerrors.ErrInvalidRequest.Wrap("group not found")
}

func (m *MockCommonsKeeper) SetExtendedGroup(ctx context.Context, name string, group commonstypes.ExtendedGroup) error {
	m.ExtendedGroups[name] = group
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

// MockGroupKeeperReg
type MockGroupKeeperReg struct {
	members        map[string]bool
	CouncilGroupId uint64
}

func (m *MockGroupKeeperReg) Reset() {
	m.members = make(map[string]bool)
	m.CouncilGroupId = 0
}

func (m *MockGroupKeeperReg) GroupsByMember(ctx context.Context, request *group.QueryGroupsByMemberRequest) (*group.QueryGroupsByMemberResponse, error) {
	if m.members[request.Address] && m.CouncilGroupId > 0 {
		return &group.QueryGroupsByMemberResponse{
			Groups: []*group.GroupInfo{
				{Id: m.CouncilGroupId},
			},
		}, nil
	}
	return &group.QueryGroupsByMemberResponse{Groups: []*group.GroupInfo{}}, nil
}

func (m *MockGroupKeeperReg) GroupPoliciesByGroup(ctx context.Context, request *group.QueryGroupPoliciesByGroupRequest) (*group.QueryGroupPoliciesByGroupResponse, error) {
	return nil, nil
}

func (m *MockGroupKeeperReg) GetGroupSequence(ctx sdk.Context) uint64 {
	return 1
}

// MockBankKeeper
type MockBankKeeper struct {
	SentCoins map[string]sdk.Coins
	sendErr   error
	hasFunds  map[string]math.Int
}

func NewMockBankKeeper() *MockBankKeeper {
	return &MockBankKeeper{
		SentCoins: make(map[string]sdk.Coins),
		hasFunds:  make(map[string]math.Int),
	}
}

func (m *MockBankKeeper) Reset() {
	m.SentCoins = make(map[string]sdk.Coins)
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
	return nil
}

func (m *MockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	if amount, ok := m.hasFunds[addr.String()]; ok {
		return sdk.NewCoins(sdk.NewCoin("uspark", amount))
	}
	return sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000000000)))
}
