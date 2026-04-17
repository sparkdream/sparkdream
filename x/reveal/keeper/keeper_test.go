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

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/keeper"
	module "sparkdream/x/reveal/module"
	"sparkdream/x/reveal/types"
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
		nil, // authKeeper - not needed for param tests
		nil, // bankKeeper - not needed for param tests
		nil, // repKeeper - not needed for param tests
		nil, // commonsKeeper - not needed for param tests
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
// Mock RepKeeper
// ---------------------------------------------------------------------------

type mockRepKeeper struct {
	isMemberFn         func(ctx context.Context, addr sdk.AccAddress) bool
	getTrustLevelFn    func(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)
	mintDREAMFn        func(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	burnDREAMFn        func(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	lockDREAMFn        func(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	unlockDREAMFn      func(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	addReputationFn    func(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error
	deductReputationFn func(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error
	createProjectFn    func(ctx context.Context, creator sdk.AccAddress, name, description string, tags []string, category reptypes.ProjectCategory, council string, requestedBudget, requestedSpark math.Int) (uint64, error)
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

func (m *mockRepKeeper) MintDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if m.mintDREAMFn != nil {
		return m.mintDREAMFn(ctx, addr, amount)
	}
	return nil
}

func (m *mockRepKeeper) BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if m.burnDREAMFn != nil {
		return m.burnDREAMFn(ctx, addr, amount)
	}
	return nil
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

func (m *mockRepKeeper) AddReputation(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error {
	if m.addReputationFn != nil {
		return m.addReputationFn(ctx, memberAddr, tag, amount)
	}
	return nil
}

func (m *mockRepKeeper) DeductReputation(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error {
	if m.deductReputationFn != nil {
		return m.deductReputationFn(ctx, memberAddr, tag, amount)
	}
	return nil
}

func (m *mockRepKeeper) CreateProject(ctx context.Context, creator sdk.AccAddress, name, description string, tags []string, category reptypes.ProjectCategory, council string, requestedBudget, requestedSpark math.Int, permissionless bool) (uint64, error) {
	if m.createProjectFn != nil {
		return m.createProjectFn(ctx, creator, name, description, tags, category, council, requestedBudget, requestedSpark)
	}
	return 1, nil
}

// ---------------------------------------------------------------------------
// Mock CommonsKeeper
// ---------------------------------------------------------------------------

type mockCommonsKeeper struct {
	isCommitteeMemberFn    func(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)
	isCouncilAuthorizedFn  func(ctx context.Context, addr string, council string, committee string) bool
}

func (m *mockCommonsKeeper) IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
	if m.isCommitteeMemberFn != nil {
		return m.isCommitteeMemberFn(ctx, address, council, committee)
	}
	return true, nil
}

func (m *mockCommonsKeeper) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if m.isCouncilAuthorizedFn != nil {
		return m.isCouncilAuthorizedFn(ctx, addr, council, committee)
	}
	return true
}

// ---------------------------------------------------------------------------
// Enhanced test fixture (used by message handler / query / endblock tests)
// ---------------------------------------------------------------------------

type testFixture struct {
	ctx             context.Context
	sdkCtx          sdk.Context
	keeper          keeper.Keeper
	msgServer       types.MsgServer
	queryServer     types.QueryServer
	repKeeper       *mockRepKeeper
	commonsKeeper   *mockCommonsKeeper
	authority       string
	contributor     string
	contributorAddr sdk.AccAddress
	staker          string
	stakerAddr      sdk.AccAddress
	staker2         string
	staker2Addr     sdk.AccAddress
}

func initTestFixture(t *testing.T) *testFixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)
	sdkCtx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	rk := &mockRepKeeper{}
	ck := &mockCommonsKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		nil, // authKeeper
		nil, // bankKeeper
		rk,
		ck,
	)

	if err := k.Params.Set(sdkCtx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	authorityStr, _ := addressCodec.BytesToString(authority)

	contributorAddr := sdk.AccAddress([]byte("contributor_________"))
	contributorStr, _ := addressCodec.BytesToString(contributorAddr)

	stakerAddr := sdk.AccAddress([]byte("staker______________"))
	stakerStr, _ := addressCodec.BytesToString(stakerAddr)

	staker2Addr := sdk.AccAddress([]byte("staker2_____________"))
	staker2Str, _ := addressCodec.BytesToString(staker2Addr)

	return &testFixture{
		ctx:             sdkCtx,
		sdkCtx:          sdkCtx,
		keeper:          k,
		msgServer:       keeper.NewMsgServerImpl(k),
		queryServer:     keeper.NewQueryServerImpl(k),
		repKeeper:       rk,
		commonsKeeper:   ck,
		authority:       authorityStr,
		contributor:     contributorStr,
		contributorAddr: contributorAddr,
		staker:          stakerStr,
		stakerAddr:      stakerAddr,
		staker2:         staker2Str,
		staker2Addr:     staker2Addr,
	}
}

// createDefaultProposal creates a two-tranche contribution in PROPOSED status.
func (f *testFixture) createDefaultProposal(t *testing.T) uint64 {
	t.Helper()
	resp, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "zenith-core",
		Description:    "Core library implementation",
		TotalValuation: math.NewInt(10000),
		Tranches: []types.TrancheDef{
			{Name: "phase-1", Description: "Initial phase", StakeThreshold: math.NewInt(5000)},
			{Name: "phase-2", Description: "Second phase", StakeThreshold: math.NewInt(5000)},
		},
		InitialLicense: "BSL-1.1",
		FinalLicense:   "Apache-2.0",
	})
	require.NoError(t, err)
	return resp.ContributionId
}

// createSingleTrancheProposal creates a single-tranche contribution in PROPOSED status.
func (f *testFixture) createSingleTrancheProposal(t *testing.T, valuation int64) uint64 {
	t.Helper()
	resp, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "aurora-lib",
		Description:    "A single-tranche project",
		TotalValuation: math.NewInt(valuation),
		Tranches: []types.TrancheDef{
			{Name: "all", Description: "Everything", StakeThreshold: math.NewInt(valuation)},
		},
		InitialLicense: "BSL-1.1",
		FinalLicense:   "Apache-2.0",
	})
	require.NoError(t, err)
	return resp.ContributionId
}

// approveContribution moves contribution from PROPOSED to IN_PROGRESS.
func (f *testFixture) approveContribution(t *testing.T, contribID uint64) {
	t.Helper()
	_, err := f.msgServer.Approve(f.ctx, &types.MsgApprove{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: contribID,
	})
	require.NoError(t, err)
}

// stakeOnTranche stakes DREAM on a tranche.
func (f *testFixture) stakeOnTranche(t *testing.T, contribID uint64, trancheID uint32, staker string, amount int64) uint64 {
	t.Helper()
	resp, err := f.msgServer.Stake(f.ctx, &types.MsgStake{
		Staker:         staker,
		ContributionId: contribID,
		TrancheId:      trancheID,
		Amount:         math.NewInt(amount),
	})
	require.NoError(t, err)
	return resp.StakeId
}

// revealTranche submits reveal data for a tranche.
func (f *testFixture) revealTranche(t *testing.T, contribID uint64, trancheID uint32) {
	t.Helper()
	_, err := f.msgServer.Reveal(f.ctx, &types.MsgReveal{
		Contributor:    f.contributor,
		ContributionId: contribID,
		TrancheId:      trancheID,
		CodeUri:        "ipfs://Qm_code_hash",
		DocsUri:        "ipfs://Qm_docs_hash",
		CommitHash:     "abc123def456",
	})
	require.NoError(t, err)
}

// castVerifyVote places a verification vote from a staker.
func (f *testFixture) castVerifyVote(t *testing.T, contribID uint64, trancheID uint32, voter string, confirmed bool, quality uint32) {
	t.Helper()
	_, err := f.msgServer.Verify(f.ctx, &types.MsgVerify{
		Voter:          voter,
		ContributionId: contribID,
		TrancheId:      trancheID,
		ValueConfirmed: confirmed,
		QualityRating:  quality,
		Comments:       "Looks good",
	})
	require.NoError(t, err)
}

// advanceBlockHeight increments the block height on the fixture context.
func (f *testFixture) advanceBlockHeight(delta int64) {
	f.sdkCtx = f.sdkCtx.WithBlockHeight(f.sdkCtx.BlockHeight() + delta)
	f.ctx = f.sdkCtx
}
