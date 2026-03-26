package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	module "sparkdream/x/shield/module"
	"sparkdream/x/shield/types"
)

// --- Enhanced Mocks for BeginBlocker tests ---

type mockBankKeeperWithBalance struct {
	balance math.Int
}

func (m mockBankKeeperWithBalance) GetBalance(_ context.Context, _ sdk.AccAddress, _ string) sdk.Coin {
	return sdk.NewCoin("uspark", m.balance)
}

func (m mockBankKeeperWithBalance) SpendableCoins(_ context.Context, _ sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin("uspark", m.balance))
}

func (m mockBankKeeperWithBalance) SendCoinsFromModuleToModule(_ context.Context, _, _ string, _ sdk.Coins) error {
	return nil
}

type mockDistrKeeper struct {
	distributeCalled bool
	lastAmount       sdk.Coins
	poolBalance      math.Int // community pool balance for GetCommunityPool
}

func (m *mockDistrKeeper) DistributeFromFeePool(_ context.Context, amount sdk.Coins, _ sdk.AccAddress) error {
	m.distributeCalled = true
	m.lastAmount = amount
	return nil
}

func (m *mockDistrKeeper) GetCommunityPool(_ context.Context) (sdk.DecCoins, error) {
	amt := m.poolBalance
	if amt.IsNil() || !amt.IsPositive() {
		return sdk.DecCoins{}, nil
	}
	return sdk.NewDecCoins(sdk.NewDecCoin("uspark", amt)), nil
}

// initFixtureWithLowBalance creates a fixture with a low bank balance for funding tests.
func initFixtureWithLowBalance(t *testing.T, balance math.Int) (*fixture, *mockDistrKeeper) {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx

	addrCodec := addresscodec.NewBech32Codec("sprkdrm")
	authority := authtypes.NewModuleAddress("gov")

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addrCodec,
		authority,
		mockAccountKeeper{},
		mockBankKeeperWithBalance{balance: balance},
	)

	mockSK := mockStakingKeeper{
		validators: []stakingtypes.Validator{
			{OperatorAddress: "sprkdrmvaloper1aaaaa"},
			{OperatorAddress: "sprkdrmvaloper1bbbbb"},
			{OperatorAddress: "sprkdrmvaloper1ccccc"},
		},
	}
	k.SetStakingKeeper(mockSK)

	distrMock := &mockDistrKeeper{poolBalance: math.NewInt(1_000_000_000)} // 1000 SPARK
	k.SetDistrKeeper(distrMock)

	err := k.InitGenesis(ctx, *types.DefaultGenesis())
	require.NoError(t, err)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addrCodec,
	}, distrMock
}

// --- BeginBlocker Tests ---

func TestBeginBlockerDisabled(t *testing.T) {
	f := initFixture(t)

	// Disable shield
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.Enabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Should return nil without error
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)
}

func TestBeginBlockerNoFundingNeeded(t *testing.T) {
	f := initFixture(t)

	// Balance (1B) >> MinGasReserve (10M), so no funding should happen
	err := f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify no day funding was recorded (no distr keeper wired = no funding)
	amount := f.keeper.GetDayFunding(f.ctx, 0)
	require.True(t, amount.IsZero())
}

func TestBeginBlockerAutoFunding(t *testing.T) {
	// Create fixture with low balance that needs funding
	lowBalance := math.NewInt(1000000) // 1M < MinGasReserve of 10M
	f, distrMock := initFixtureWithLowBalance(t, lowBalance)

	err := f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// DistrKeeper should have been called for funding
	require.True(t, distrMock.distributeCalled)
}

func TestBeginBlockerEmptyPool(t *testing.T) {
	// Create fixture with low balance that needs funding
	lowBalance := math.NewInt(1000000) // 1M < MinGasReserve of 10M
	f, distrMock := initFixtureWithLowBalance(t, lowBalance)

	// Set community pool to zero — shield should skip funding silently
	distrMock.poolBalance = math.ZeroInt()

	err := f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// DistrKeeper.DistributeFromFeePool should NOT have been called
	require.False(t, distrMock.distributeCalled)

	// No day funding recorded
	day := uint64(f.ctx.BlockHeight()) / 14400
	amount := f.keeper.GetDayFunding(f.ctx, day)
	require.True(t, amount.IsZero())
}

func TestBeginBlockerInsufficientPool(t *testing.T) {
	// Create fixture with low balance that needs funding
	lowBalance := math.NewInt(1000000) // 1M uspark
	f, distrMock := initFixtureWithLowBalance(t, lowBalance)

	// Pool has less than the gap (gap = MinGasReserve - balance = 10M - 1M = 9M)
	distrMock.poolBalance = math.NewInt(5000000) // 5M < 9M gap

	err := f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Should NOT fund — pool balance < required amount
	require.False(t, distrMock.distributeCalled)
}

func TestBeginBlockerFundingDayCap(t *testing.T) {
	lowBalance := math.NewInt(1000000)
	f, distrMock := initFixtureWithLowBalance(t, lowBalance)

	// Set day funding to max already
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	day := uint64(f.ctx.BlockHeight()) / 14400
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, day, params.MaxFundingPerDay))

	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Should NOT have called distribute since day cap is reached
	require.False(t, distrMock.distributeCalled)
}

// --- DKG State Machine Tests ---

func TestDKGAutoTrigger(t *testing.T) {
	f := initFixture(t)

	// Verify no DKG state initially (or if default genesis set one, check it)
	// The fixture has 5 validators and MinTleValidators=5, and no TLE key set
	// So auto-trigger should fire on first BeginBlocker

	err := f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// DKG should be in REGISTERING phase
	dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, types.DKGPhase_DKG_PHASE_REGISTERING, dkgState.Phase)
	require.Equal(t, uint64(1), dkgState.Round)
	require.Len(t, dkgState.ExpectedValidators, 5)
}

func TestDKGNoAutoTriggerWhenKeySetExists(t *testing.T) {
	f := initFixture(t)

	// Set an existing TLE key set — auto-trigger should not fire
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("master_pk"),
	}))

	err := f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// DKG should not have been triggered
	_, found := f.keeper.GetDKGStateVal(f.ctx)
	require.False(t, found)
}

func TestDKGRegistrationToContributing(t *testing.T) {
	f := initFixture(t)

	// Set DKG state in REGISTERING phase with registration deadline at block 50
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
		Round:                1,
		Phase:                types.DKGPhase_DKG_PHASE_REGISTERING,
		RegistrationDeadline: 50,
		ContributionDeadline: 100,
		ExpectedValidators:   []string{"val1", "val2", "val3"},
	}))

	// Block height < deadline — should stay in REGISTERING
	err := f.keeper.BeginBlocker(f.ctx) // default block height is 0
	require.NoError(t, err)

	dkgState, _ := f.keeper.GetDKGStateVal(f.ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_REGISTERING, dkgState.Phase)

	// Advance block height past deadline
	f.ctx = f.ctx.WithBlockHeight(51)
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Should transition to CONTRIBUTING
	dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, types.DKGPhase_DKG_PHASE_CONTRIBUTING, dkgState.Phase)
}

func TestDKGContributingInsufficientFails(t *testing.T) {
	f := initFixture(t)

	// Set DKG state in CONTRIBUTING phase with deadline expired, 0 contributions
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
		Round:                 1,
		Phase:                 types.DKGPhase_DKG_PHASE_CONTRIBUTING,
		ContributionDeadline:  10,
		ThresholdNumerator:    2,
		ThresholdDenominator:  3,
		ExpectedValidators:    []string{"val1", "val2", "val3"},
		ContributionsReceived: 0,
	}))

	// Advance past contribution deadline
	f.ctx = f.ctx.WithBlockHeight(11)
	err := f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Should fail and reset to INACTIVE
	dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, types.DKGPhase_DKG_PHASE_INACTIVE, dkgState.Phase)
}

func TestDKGDriftDetection(t *testing.T) {
	f := initFixture(t)

	// Set DKG state in ACTIVE phase with a validator that's no longer bonded
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
		Round:              1,
		Phase:              types.DKGPhase_DKG_PHASE_ACTIVE,
		ExpectedValidators: []string{"val_gone1", "val_gone2", "sprkdrmvaloper1aaaaa"},
	}))

	// Set TLE key set so drift detection matters
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("mpk"),
	}))

	// Set drift threshold low enough to trigger
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxValidatorSetDrift = 50 // 50% threshold
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// 2/3 validators are gone = ~66% drift > 50% threshold
	// DKG should reset to INACTIVE
	dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, types.DKGPhase_DKG_PHASE_INACTIVE, dkgState.Phase)

	// TLE key set should be cleared
	_, found = f.keeper.GetTLEKeySetVal(f.ctx)
	require.False(t, found)

	// Encrypted batch should be disabled
	updatedParams, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.False(t, updatedParams.EncryptedBatchEnabled)
}

func TestDKGNoDriftBelowThreshold(t *testing.T) {
	f := initFixture(t)

	// All 3 expected validators are still bonded
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
		Round:              1,
		Phase:              types.DKGPhase_DKG_PHASE_ACTIVE,
		ExpectedValidators: []string{"sprkdrmvaloper1aaaaa", "sprkdrmvaloper1bbbbb", "sprkdrmvaloper1ccccc"},
	}))

	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("mpk"),
	}))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxValidatorSetDrift = 50
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// 0% drift < 50% threshold — should stay ACTIVE
	dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, types.DKGPhase_DKG_PHASE_ACTIVE, dkgState.Phase)

	// TLE key set should remain
	_, found = f.keeper.GetTLEKeySetVal(f.ctx)
	require.True(t, found)
}
