package keeper_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/keeper"
	module "sparkdream/x/vote/module"
	"sparkdream/x/vote/types"
	zkcrypto "sparkdream/zkprivatevoting/crypto"
)

// testTreeDepth uses a small Merkle tree depth for tests. The production
// depth of 20 requires ~1M MiMC hashes in Build() which is far too slow
// for unit tests. Depth 3 (max 8 leaves) is sufficient for all test cases.
const testTreeDepth = 3

func init() {
	keeper.SetBuildMerkleTreeFunc(func(zkPubKeys [][]byte) ([]byte, uint64) {
		if len(zkPubKeys) == 0 {
			return nil, 0
		}
		tree := zkcrypto.NewMerkleTree(testTreeDepth)
		for _, pubKey := range zkPubKeys {
			leaf := zkcrypto.ComputeLeaf(pubKey, 1)
			tree.AddLeaf(leaf) //nolint:errcheck
		}
		tree.Build() //nolint:errcheck
		return tree.Root(), uint64(len(zkPubKeys))
	})
	keeper.SetBuildMerkleTreeFullFunc(func(zkPubKeys [][]byte) *zkcrypto.MerkleTree {
		tree := zkcrypto.NewMerkleTree(testTreeDepth)
		for _, pubKey := range zkPubKeys {
			leaf := zkcrypto.ComputeLeaf(pubKey, 1)
			tree.AddLeaf(leaf) //nolint:errcheck
		}
		if len(zkPubKeys) > 0 {
			tree.Build() //nolint:errcheck
		}
		return tree
	})
}

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
		nil, // authKeeper
		nil, // bankKeeper
		nil, // repKeeper
		nil, // seasonKeeper
		nil, // stakingKeeper
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
// Mock AuthKeeper
// ---------------------------------------------------------------------------

type mockAuthKeeper struct {
	addressCodecFn func() address.Codec
	getAccountFn   func(context.Context, sdk.AccAddress) sdk.AccountI
}

func (m *mockAuthKeeper) AddressCodec() address.Codec {
	if m.addressCodecFn != nil {
		return m.addressCodecFn()
	}
	return addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
}

func (m *mockAuthKeeper) GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI {
	if m.getAccountFn != nil {
		return m.getAccountFn(ctx, addr)
	}
	return nil // not a module account by default
}

// ---------------------------------------------------------------------------
// Mock BankKeeper
// ---------------------------------------------------------------------------

type mockBankKeeper struct {
	spendableCoinsFn func(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}

func (m *mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	if m.spendableCoinsFn != nil {
		return m.spendableCoinsFn(ctx, addr)
	}
	return sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000_000_000)))
}

// ---------------------------------------------------------------------------
// Mock RepKeeper
// ---------------------------------------------------------------------------

type mockRepKeeper struct {
	isMemberFn func(ctx context.Context, addr sdk.AccAddress) bool
}

func (m *mockRepKeeper) IsMember(ctx context.Context, addr sdk.AccAddress) bool {
	if m.isMemberFn != nil {
		return m.isMemberFn(ctx, addr)
	}
	return true
}

// ---------------------------------------------------------------------------
// Mock SeasonKeeper
// ---------------------------------------------------------------------------

type mockSeasonKeeper struct {
	getCurrentEpochFn func(ctx context.Context) int64
}

func (m *mockSeasonKeeper) GetCurrentEpoch(ctx context.Context) int64 {
	if m.getCurrentEpochFn != nil {
		return m.getCurrentEpochFn(ctx)
	}
	return 10
}

// ---------------------------------------------------------------------------
// Mock StakingKeeper
// ---------------------------------------------------------------------------

type mockStakingKeeper struct {
	getValidatorFn func(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	jailFn         func(ctx context.Context, consAddr sdk.ConsAddress) error
	jailCalls      []sdk.ConsAddress // tracks Jail calls for assertions
}

func (m *mockStakingKeeper) GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	if m.getValidatorFn != nil {
		return m.getValidatorFn(ctx, addr)
	}
	// Return bonded validator by default.
	return stakingtypes.Validator{
		Status: stakingtypes.Bonded,
	}, nil
}

func (m *mockStakingKeeper) Jail(ctx context.Context, consAddr sdk.ConsAddress) error {
	m.jailCalls = append(m.jailCalls, consAddr)
	if m.jailFn != nil {
		return m.jailFn(ctx, consAddr)
	}
	return nil
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
	authKeeper    *mockAuthKeeper
	bankKeeper    *mockBankKeeper
	repKeeper     *mockRepKeeper
	seasonKeeper  *mockSeasonKeeper
	stakingKeeper *mockStakingKeeper
	addressCodec  address.Codec
	authority     string
	member        string
	memberAddr    sdk.AccAddress
	member2       string
	member2Addr   sdk.AccAddress
	nonMember     string
	nonMemberAddr sdk.AccAddress
	validator     string
	validatorAddr sdk.AccAddress
}

func initTestFixture(t *testing.T) *testFixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	ac := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)
	sdkCtx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	ak := &mockAuthKeeper{}
	bk := &mockBankKeeper{}
	rk := &mockRepKeeper{}
	sk := &mockSeasonKeeper{}
	stk := &mockStakingKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		ac,
		authority,
		ak,
		bk,
		rk,
		sk,
		stk,
	)

	if err := k.Params.Set(sdkCtx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	authorityStr, _ := ac.BytesToString(authority)

	memberAddr := sdk.AccAddress([]byte("member______________"))
	memberStr, _ := ac.BytesToString(memberAddr)

	member2Addr := sdk.AccAddress([]byte("member2_____________"))
	member2Str, _ := ac.BytesToString(member2Addr)

	nonMemberAddr := sdk.AccAddress([]byte("nonmember___________"))
	nonMemberStr, _ := ac.BytesToString(nonMemberAddr)

	validatorAddr := sdk.AccAddress([]byte("validator___________"))
	validatorStr, _ := ac.BytesToString(validatorAddr)

	// Configure repKeeper: member, member2, validator are members; nonMember is not
	rk.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
		return addr.Equals(memberAddr) || addr.Equals(member2Addr) || addr.Equals(validatorAddr)
	}

	return &testFixture{
		ctx:           sdkCtx,
		sdkCtx:        sdkCtx,
		keeper:        k,
		msgServer:     keeper.NewMsgServerImpl(k),
		queryServer:   keeper.NewQueryServerImpl(k),
		authKeeper:    ak,
		bankKeeper:    bk,
		repKeeper:     rk,
		seasonKeeper:  sk,
		stakingKeeper: stk,
		addressCodec:  ac,
		authority:     authorityStr,
		member:        memberStr,
		memberAddr:    memberAddr,
		member2:       member2Str,
		member2Addr:   member2Addr,
		nonMember:     nonMemberStr,
		nonMemberAddr: nonMemberAddr,
		validator:     validatorStr,
		validatorAddr: validatorAddr,
	}
}

// ---------------------------------------------------------------------------
// Helper methods on testFixture
// ---------------------------------------------------------------------------

// registerVoter registers a voter via the msg server.
func (f *testFixture) registerVoter(t *testing.T, addr string, zkPubKey []byte) {
	t.Helper()
	_, err := f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
		Voter:       addr,
		ZkPublicKey: zkPubKey,
	})
	require.NoError(t, err)
}

// createPublicProposal creates a minimal PUBLIC proposal with 2 standard options.
func (f *testFixture) createPublicProposal(t *testing.T, proposer string) uint64 {
	t.Helper()
	resp, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
		Proposer:   proposer,
		Title:      "Test Proposal",
		Options:    f.standardOptions(),
		Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
		Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
	})
	require.NoError(t, err)
	return resp.ProposalId
}

// createSealedProposal creates a minimal SEALED proposal with 2 standard options.
func (f *testFixture) createSealedProposal(t *testing.T, proposer string) uint64 {
	t.Helper()
	resp, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
		Proposer:   proposer,
		Title:      "Sealed Proposal",
		Options:    f.standardOptions(),
		Visibility: types.VisibilityLevel_VISIBILITY_SEALED,
		Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
	})
	require.NoError(t, err)
	return resp.ProposalId
}

// standardOptions returns Yes/No standard options.
func (f *testFixture) standardOptions() []*types.VoteOption {
	return []*types.VoteOption{
		{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
		{Id: 1, Label: "No", Role: types.OptionRole_OPTION_ROLE_STANDARD},
	}
}

// optionsWithAbstainVeto returns Yes/No/Abstain/Veto options.
func (f *testFixture) optionsWithAbstainVeto() []*types.VoteOption {
	return []*types.VoteOption{
		{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
		{Id: 1, Label: "No", Role: types.OptionRole_OPTION_ROLE_STANDARD},
		{Id: 2, Label: "Abstain", Role: types.OptionRole_OPTION_ROLE_ABSTAIN},
		{Id: 3, Label: "Veto", Role: types.OptionRole_OPTION_ROLE_VETO},
	}
}

// setBlockHeight sets the block height on the fixture context.
func (f *testFixture) setBlockHeight(h int64) {
	f.sdkCtx = f.sdkCtx.WithBlockHeight(h)
	f.ctx = f.sdkCtx
}

// genZkPubKey generates a deterministic 32-byte key from a seed.
func genZkPubKey(seed int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(seed))
	h := sha256.Sum256(b)
	return h[:]
}

// genNullifier generates a deterministic 32-byte nullifier from a seed.
func genNullifier(seed int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(seed)+1000)
	h := sha256.Sum256(b)
	return h[:]
}
