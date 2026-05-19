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

	"sparkdream/x/service/keeper"
	module "sparkdream/x/service/module"
	"sparkdream/x/service/types"
)

// ---------------------------------------------------------------------------
// Test addresses
// ---------------------------------------------------------------------------

var (
	testAddrCodec address.Codec

	testOperator1Addr  sdk.AccAddress
	testOperator1      string
	testOperator2Addr  sdk.AccAddress
	testOperator2      string
	testControllerAddr sdk.AccAddress
	testController     string
	testReporterAddr   sdk.AccAddress
	testReporter      string
	testCouncilAddr    sdk.AccAddress
	testCouncil       string
	testRandomAddr     sdk.AccAddress
	testRandom        string
)

func init() {
	testAddrCodec = addresscodec.NewBech32Codec("cosmos")

	// Deterministic per-byte-pattern addresses with valid bech32 checksums.
	testOperator1Addr = sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	testOperator1, _ = testAddrCodec.BytesToString(testOperator1Addr)
	testOperator2Addr = sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	testOperator2, _ = testAddrCodec.BytesToString(testOperator2Addr)
	testControllerAddr = sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	testController, _ = testAddrCodec.BytesToString(testControllerAddr)
	testReporterAddr = sdk.AccAddress(bytes.Repeat([]byte{4}, 20))
	testReporter, _ = testAddrCodec.BytesToString(testReporterAddr)
	testCouncilAddr = sdk.AccAddress(bytes.Repeat([]byte{5}, 20))
	testCouncil, _ = testAddrCodec.BytesToString(testCouncilAddr)
	testRandomAddr = sdk.AccAddress(bytes.Repeat([]byte{6}, 20))
	testRandom, _ = testAddrCodec.BytesToString(testRandomAddr)
}

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// mockBankKeeper implements types.BankKeeper for testing.
type mockBankKeeper struct {
	SpendableCoinsFn               func(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromAccountToModuleFn func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccountFn func(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModuleFn  func(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	GetBalanceFn                   func(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin

	AcctToModCalls []bankCall
	ModToAcctCalls []bankCall
	ModToModCalls  []bankModToModCall
}

type bankCall struct {
	Sender    sdk.AccAddress
	Recipient sdk.AccAddress
	Module    string
	Amt       sdk.Coins
}

type bankModToModCall struct {
	SenderModule    string
	RecipientModule string
	Amt             sdk.Coins
}

func (m *mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	if m.SpendableCoinsFn != nil {
		return m.SpendableCoinsFn(ctx, addr)
	}
	return sdk.NewCoins(sdk.NewCoin(types.BondDenom, math.NewInt(1_000_000_000_000)))
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	m.AcctToModCalls = append(m.AcctToModCalls, bankCall{Sender: senderAddr, Module: recipientModule, Amt: amt})
	if m.SendCoinsFromAccountToModuleFn != nil {
		return m.SendCoinsFromAccountToModuleFn(ctx, senderAddr, recipientModule, amt)
	}
	return nil
}

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	m.ModToAcctCalls = append(m.ModToAcctCalls, bankCall{Module: senderModule, Recipient: recipientAddr, Amt: amt})
	if m.SendCoinsFromModuleToAccountFn != nil {
		return m.SendCoinsFromModuleToAccountFn(ctx, senderModule, recipientAddr, amt)
	}
	return nil
}

func (m *mockBankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	m.ModToModCalls = append(m.ModToModCalls, bankModToModCall{SenderModule: senderModule, RecipientModule: recipientModule, Amt: amt})
	if m.SendCoinsFromModuleToModuleFn != nil {
		return m.SendCoinsFromModuleToModuleFn(ctx, senderModule, recipientModule, amt)
	}
	return nil
}

func (m *mockBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	if m.GetBalanceFn != nil {
		return m.GetBalanceFn(ctx, addr, denom)
	}
	return sdk.NewCoin(denom, math.NewInt(1_000_000_000_000))
}

// mockCommonsKeeper implements types.CommonsKeeper for testing.
type mockCommonsKeeper struct {
	IsGroupPolicyAddressFn   func(ctx context.Context, addr string) bool
	IsGroupPolicyMemberFn    func(ctx context.Context, policyAddr string, memberAddr string) (bool, error)
	GroupPolicyMemberCountFn func(ctx context.Context, policyAddr string) (uint64, error)
	IsCouncilAuthorizedFn    func(ctx context.Context, addr string, council string, committee string) bool
}

func (m *mockCommonsKeeper) IsGroupPolicyAddress(ctx context.Context, addr string) bool {
	if m.IsGroupPolicyAddressFn != nil {
		return m.IsGroupPolicyAddressFn(ctx, addr)
	}
	return false
}

func (m *mockCommonsKeeper) IsGroupPolicyMember(ctx context.Context, policyAddr string, memberAddr string) (bool, error) {
	if m.IsGroupPolicyMemberFn != nil {
		return m.IsGroupPolicyMemberFn(ctx, policyAddr, memberAddr)
	}
	return false, nil
}

func (m *mockCommonsKeeper) GroupPolicyMemberCount(ctx context.Context, policyAddr string) (uint64, error) {
	if m.GroupPolicyMemberCountFn != nil {
		return m.GroupPolicyMemberCountFn(ctx, policyAddr)
	}
	return 1, nil
}

func (m *mockCommonsKeeper) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if m.IsCouncilAuthorizedFn != nil {
		return m.IsCouncilAuthorizedFn(ctx, addr, council, committee)
	}
	return false
}

// mockRepKeeper implements types.RepKeeper for testing.
type mockRepKeeper struct {
	AddReputationFn          func(ctx context.Context, address sdk.AccAddress, tag string, amount math.LegacyDec) error
	DeductReputationFn       func(ctx context.Context, address sdk.AccAddress, tag string, amount math.LegacyDec) error
	CreateAppealInitiativeFn func(ctx context.Context, initiativeType string, payload []byte, deadline int64) (uint64, error)
	GetJuryVerdictNameFn     func(ctx context.Context, juryReviewID uint64) (string, error)
	MeetsTrustLevelFn        func(ctx context.Context, address sdk.AccAddress, minLevelName string) (bool, error)

	AddReputationCalls    []repGrantCall
	DeductReputationCalls []repGrantCall
	NextInitiativeID      uint64
}

type repGrantCall struct {
	Address sdk.AccAddress
	Tag     string
	Amount  math.LegacyDec
}

func (m *mockRepKeeper) AddReputation(ctx context.Context, address sdk.AccAddress, tag string, amount math.LegacyDec) error {
	m.AddReputationCalls = append(m.AddReputationCalls, repGrantCall{Address: address, Tag: tag, Amount: amount})
	if m.AddReputationFn != nil {
		return m.AddReputationFn(ctx, address, tag, amount)
	}
	return nil
}

func (m *mockRepKeeper) DeductReputation(ctx context.Context, address sdk.AccAddress, tag string, amount math.LegacyDec) error {
	m.DeductReputationCalls = append(m.DeductReputationCalls, repGrantCall{Address: address, Tag: tag, Amount: amount})
	if m.DeductReputationFn != nil {
		return m.DeductReputationFn(ctx, address, tag, amount)
	}
	return nil
}

func (m *mockRepKeeper) CreateAppealInitiative(ctx context.Context, initiativeType string, payload []byte, deadline int64) (uint64, error) {
	if m.CreateAppealInitiativeFn != nil {
		return m.CreateAppealInitiativeFn(ctx, initiativeType, payload, deadline)
	}
	m.NextInitiativeID++
	return m.NextInitiativeID, nil
}

func (m *mockRepKeeper) GetJuryVerdictName(ctx context.Context, juryReviewID uint64) (string, error) {
	if m.GetJuryVerdictNameFn != nil {
		return m.GetJuryVerdictNameFn(ctx, juryReviewID)
	}
	return types.JuryVerdictUpholdChallenge, nil
}

func (m *mockRepKeeper) MeetsTrustLevel(ctx context.Context, address sdk.AccAddress, minLevelName string) (bool, error) {
	if m.MeetsTrustLevelFn != nil {
		return m.MeetsTrustLevelFn(ctx, address, minLevelName)
	}
	return true, nil
}

// mockDistributionKeeper implements types.DistributionKeeper for testing.
type mockDistributionKeeper struct {
	FundCommunityPoolFn func(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
	Calls               []distCall
}

type distCall struct {
	Amount sdk.Coins
	Sender sdk.AccAddress
}

func (m *mockDistributionKeeper) FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error {
	m.Calls = append(m.Calls, distCall{Amount: amount, Sender: sender})
	if m.FundCommunityPoolFn != nil {
		return m.FundCommunityPoolFn(ctx, amount, sender)
	}
	return nil
}

// mockAuthKeeper implements types.AuthKeeper for testing. Provides
// deterministic ModuleName → AccAddress resolution for OpenSystemReport
// caller authorization tests.
type mockAuthKeeper struct {
	GetModuleAddressFn func(name string) sdk.AccAddress
	addressCodec       address.Codec
}

func (m *mockAuthKeeper) AddressCodec() address.Codec {
	return m.addressCodec
}

func (m *mockAuthKeeper) GetAccount(_ context.Context, _ sdk.AccAddress) sdk.AccountI {
	return nil
}

func (m *mockAuthKeeper) GetModuleAddress(name string) sdk.AccAddress {
	if m.GetModuleAddressFn != nil {
		return m.GetModuleAddressFn(name)
	}
	// Default deterministic addresses for known modules. Matches what
	// authtypes.NewModuleAddress would produce in real wiring, but the
	// concrete bytes only need to be stable within a test run.
	return authtypes.NewModuleAddress(name).Bytes()
}

// mockServiceHooks captures hook fires for assertions in tests.
type mockServiceHooks struct {
	Dissolved   []dissolveCall
	Retired     []dissolveCall
	Underfunded []dissolveCall
	ReFunded    []dissolveCall
}

type dissolveCall struct {
	Operator    sdk.AccAddress
	ServiceType string
}

func (m *mockServiceHooks) AfterOperatorDissolved(_ context.Context, operator sdk.AccAddress, serviceType string) {
	m.Dissolved = append(m.Dissolved, dissolveCall{Operator: operator, ServiceType: serviceType})
}

func (m *mockServiceHooks) AfterOperatorRetired(_ context.Context, operator sdk.AccAddress, serviceType string) {
	m.Retired = append(m.Retired, dissolveCall{Operator: operator, ServiceType: serviceType})
}

func (m *mockServiceHooks) AfterOperatorUnderfunded(_ context.Context, operator sdk.AccAddress, serviceType string) {
	m.Underfunded = append(m.Underfunded, dissolveCall{Operator: operator, ServiceType: serviceType})
}

func (m *mockServiceHooks) AfterOperatorReFunded(_ context.Context, operator sdk.AccAddress, serviceType string) {
	m.ReFunded = append(m.ReFunded, dissolveCall{Operator: operator, ServiceType: serviceType})
}

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

type fixture struct {
	ctx                context.Context
	keeper             keeper.Keeper
	addressCodec       address.Codec
	msgServer          types.MsgServer
	queryServer        types.QueryServer
	bankKeeper         *mockBankKeeper
	commonsKeeper      *mockCommonsKeeper
	repKeeper          *mockRepKeeper
	distributionKeeper *mockDistributionKeeper
	authKeeper         *mockAuthKeeper
	hooks              *mockServiceHooks
	authorityStr       string
}

// initFixture wires up a fully-mocked Keeper with permissive defaults.
// Individual tests override mock Fn fields to exercise gates.
func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	sdkCtx := sdk.UnwrapSDKContext(ctx).WithBlockHeight(100)
	ctx = sdkCtx

	authority := authtypes.NewModuleAddress(types.GovModuleName)
	authorityStr, err := addressCodec.BytesToString(authority.Bytes())
	if err != nil {
		t.Fatalf("authority bech32 encode: %v", err)
	}

	bankKeeper := &mockBankKeeper{}
	authKeeper := &mockAuthKeeper{addressCodec: addressCodec}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority.Bytes(),
		bankKeeper,
		authKeeper,
	)

	// Cross-module mocks. Permissive defaults: controller is a group,
	// jury verdict is UPHOLD, council member is testCouncil only, trust
	// level always passes.
	commonsKeeper := &mockCommonsKeeper{}
	commonsKeeper.IsGroupPolicyAddressFn = func(_ context.Context, addr string) bool {
		// testController is "a Group policy address"; everything else is
		// not, so self-controller / EOA-controller checks have something
		// to reject against.
		return addr == testController
	}
	commonsKeeper.IsGroupPolicyMemberFn = func(_ context.Context, _ string, memberAddr string) (bool, error) {
		return false, nil
	}
	commonsKeeper.GroupPolicyMemberCountFn = func(_ context.Context, _ string) (uint64, error) {
		return 1, nil
	}
	commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, addr string, _ string, _ string) bool {
		return addr == testCouncil
	}

	repKeeper := &mockRepKeeper{}
	distributionKeeper := &mockDistributionKeeper{}
	hooks := &mockServiceHooks{}

	k.SetCrossModuleKeepers(commonsKeeper, repKeeper, distributionKeeper)
	k.SetHooks(hooks)

	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &fixture{
		ctx:                ctx,
		keeper:             k,
		addressCodec:       addressCodec,
		msgServer:          keeper.NewMsgServerImpl(k),
		queryServer:        keeper.NewQueryServerImpl(k),
		bankKeeper:         bankKeeper,
		commonsKeeper:      commonsKeeper,
		repKeeper:          repKeeper,
		distributionKeeper: distributionKeeper,
		authKeeper:         authKeeper,
		hooks:              hooks,
		authorityStr:       authorityStr,
	}
}

func (f *fixture) sdkCtx() sdk.Context {
	return sdk.UnwrapSDKContext(f.ctx)
}

func (f *fixture) withBlockHeight(h int64) {
	f.ctx = f.sdkCtx().WithBlockHeight(h)
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

const testServiceType = "test-service"

// seedServiceType inserts a permissive ServiceTypeConfig under
// testServiceType.
func (f *fixture) seedServiceType(t *testing.T) types.ServiceTypeConfig {
	t.Helper()
	cfg := types.ServiceTypeConfig{
		ServiceType:            testServiceType,
		Description:            "unit-test service type",
		MinBond:                sdk.NewCoin(types.BondDenom, math.NewInt(1_000_000)),
		UnbondingPeriodBlocks:  20,
		UnilateralSlashCapBps:  500,
		Tier1WindowBlocks:      1000,
		Tier1AggregateCapBps:   1500,
		Tier1CooldownBlocks:    10,
		UnderfundedGraceBlocks: 10,
		Enabled:                true,
	}
	if err := f.keeper.ServiceTypes.Set(f.ctx, testServiceType, cfg); err != nil {
		t.Fatalf("seedServiceType: %v", err)
	}
	return cfg
}

// seedActiveOperator inserts an ACTIVE operator record. Caller must have
// already seeded a service type (or pass true for createServiceType).
func (f *fixture) seedActiveOperator(t *testing.T, addr string, controller string, bond math.Int) types.Operator {
	t.Helper()
	height := f.sdkCtx().BlockHeight()
	op := types.Operator{
		Address:                 addr,
		ServiceType:             testServiceType,
		Controller:              controller,
		Bond:                    sdk.NewCoin(types.BondDenom, bond),
		Metadata:                []byte("seed-metadata"),
		Status:                  types.OperatorStatus_OPERATOR_STATUS_ACTIVE,
		Tier1SlashedInWindow:    math.ZeroInt(),
		Tier1WindowStart:        height,
		Tier1WindowStartBond:    bond,
		RegisteredAt:            height,
		TotalLifetimeBondBlocks: math.ZeroInt(),
		LastBondBlockUpdateAt:   height,
	}
	if err := f.keeper.PutOperator(f.ctx, op); err != nil {
		t.Fatalf("seedActiveOperator: %v", err)
	}
	return op
}
