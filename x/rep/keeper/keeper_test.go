package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/rep/keeper"
	module "sparkdream/x/rep/module"
	"sparkdream/x/rep/types"
)

type mockCommonsKeeper struct {
	IsCommitteeMemberFn     func(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)
	GetCommitteeGroupInfoFn func(ctx context.Context, council string, committee string) (interface{}, error)
	IsCouncilAuthorizedFn   func(ctx context.Context, addr string, council string, committee string) bool
}

func (m mockCommonsKeeper) IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
	if m.IsCommitteeMemberFn != nil {
		return m.IsCommitteeMemberFn(ctx, address, council, committee)
	}
	return false, nil
}

func (m mockCommonsKeeper) GetCommitteeGroupInfo(ctx context.Context, council string, committee string) (interface{}, error) {
	if m.GetCommitteeGroupInfoFn != nil {
		return m.GetCommitteeGroupInfoFn(ctx, council, committee)
	}
	return nil, nil
}

func (m mockCommonsKeeper) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if m.IsCouncilAuthorizedFn != nil {
		return m.IsCouncilAuthorizedFn(ctx, addr, council, committee)
	}
	return false
}

type mockSeasonKeeper struct {
	GetCurrentSeasonFn func(ctx context.Context) (types.SeasonState, error)
}

func (m mockSeasonKeeper) GetCurrentSeason(ctx context.Context) (types.SeasonState, error) {
	if m.GetCurrentSeasonFn != nil {
		return m.GetCurrentSeasonFn(ctx)
	}
	// Default: return season 1
	return types.SeasonState{Number: 1}, nil
}

func (m mockSeasonKeeper) ResolveDisplayNameAppealInternal(ctx context.Context, member string, appealSucceeded bool) error {
	return nil
}

type mockVoteKeeper struct {
	VerifyMembershipProofFn      func(ctx context.Context, proof []byte, nullifier []byte) error
	GetActiveVoterZkPublicKeysFn func(ctx context.Context) ([]string, [][]byte, error)
	GetVoterZkPublicKeyFn        func(ctx context.Context, address string) ([]byte, error)
}

func (m mockVoteKeeper) VerifyMembershipProof(ctx context.Context, proof []byte, nullifier []byte) error {
	if m.VerifyMembershipProofFn != nil {
		return m.VerifyMembershipProofFn(ctx, proof, nullifier)
	}
	// Default: accept any non-empty proof (dev mode behavior)
	if len(proof) == 0 {
		return fmt.Errorf("empty proof")
	}
	return nil
}

func (m mockVoteKeeper) GetActiveVoterZkPublicKeys(ctx context.Context) ([]string, [][]byte, error) {
	if m.GetActiveVoterZkPublicKeysFn != nil {
		return m.GetActiveVoterZkPublicKeysFn(ctx)
	}
	// Default: no registered voters
	return nil, nil, nil
}

func (m mockVoteKeeper) GetVoterZkPublicKey(ctx context.Context, address string) ([]byte, error) {
	if m.GetVoterZkPublicKeyFn != nil {
		return m.GetVoterZkPublicKeyFn(ctx, address)
	}
	// Default: no registration found
	return nil, fmt.Errorf("no voter registration for %s", address)
}

type fixture struct {
	ctx           sdk.Context
	keeper        keeper.Keeper
	addressCodec  address.Codec
	bankKeeper    *mockBankKeeper
	commonsKeeper *mockCommonsKeeper
	seasonKeeper  *mockSeasonKeeper
	voteKeeper    *mockVoteKeeper
}

// fixtureOptions allows customization of test fixture
type fixtureOptions struct {
	customParams        *types.Params
	authorizationPolicy func(address sdk.AccAddress, council string, committee string) bool
	seasonNumber        uint64
}

// FixtureOption is a function that configures fixture options
type FixtureOption func(*fixtureOptions)

// WithCustomParams sets custom params for the fixture
func WithCustomParams(params types.Params) FixtureOption {
	return func(opts *fixtureOptions) {
		opts.customParams = &params
	}
}

// WithAuthorizationPolicy sets a custom authorization policy
// The policy function receives (address, council, committee) and returns whether the address is authorized
func WithAuthorizationPolicy(policy func(sdk.AccAddress, string, string) bool) FixtureOption {
	return func(opts *fixtureOptions) {
		opts.authorizationPolicy = policy
	}
}

// WithSeasonNumber sets the current season number for the mock
func WithSeasonNumber(season uint64) FixtureOption {
	return func(opts *fixtureOptions) {
		opts.seasonNumber = season
	}
}

// AlwaysAuthorized is a convenience policy that always authorizes
func AlwaysAuthorized(_ sdk.AccAddress, _ string, _ string) bool {
	return true
}

// NeverAuthorized is a convenience policy that never authorizes
func NeverAuthorized(_ sdk.AccAddress, _ string, _ string) bool {
	return false
}

// AuthorizeAddresses is a convenience policy that authorizes only specific addresses
func AuthorizeAddresses(authorizedAddrs ...sdk.AccAddress) func(sdk.AccAddress, string, string) bool {
	authSet := make(map[string]bool, len(authorizedAddrs))
	for _, addr := range authorizedAddrs {
		authSet[addr.String()] = true
	}
	return func(addr sdk.AccAddress, _ string, _ string) bool {
		return authSet[addr.String()]
	}
}

func initFixture(t *testing.T, opts ...FixtureOption) *fixture {
	t.Helper()

	// Apply options
	options := &fixtureOptions{
		authorizationPolicy: AlwaysAuthorized, // Default: authorize everyone
		seasonNumber:        0,                // Default: season 0 (matches default LastCreditResetSeason)
	}
	for _, opt := range opts {
		opt(options)
	}

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	// Mock CommonsKeeper with configurable authorization
	commonsKeeper := &mockCommonsKeeper{
		IsCommitteeMemberFn: func(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
			return options.authorizationPolicy(address, council, committee), nil
		},
		GetCommitteeGroupInfoFn: func(ctx context.Context, council string, committee string) (interface{}, error) {
			return nil, nil
		},
	}

	// Mock SeasonKeeper with configurable season number
	seasonKeeper := &mockSeasonKeeper{
		GetCurrentSeasonFn: func(ctx context.Context) (types.SeasonState, error) {
			return types.SeasonState{Number: options.seasonNumber}, nil
		},
	}

	// Mock VoteKeeper (accepts any non-empty proof by default)
	voteKeeper := &mockVoteKeeper{}

	// Mock BankKeeper (all operations succeed by default)
	bankKeeper := &mockBankKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		nil,
		bankKeeper,
		commonsKeeper,
		voteKeeper,
	)
	k.SetSeasonKeeper(seasonKeeper)

	// Initialize genesis with default values (including sequence counters starting at 1)
	genState := types.DefaultGenesis()
	if options.customParams != nil {
		genState.Params = *options.customParams
	}
	if err := k.InitGenesis(ctx, *genState); err != nil {
		t.Fatalf("failed to init genesis: %v", err)
	}

	return &fixture{
		ctx:           ctx,
		keeper:        k,
		addressCodec:  addressCodec,
		bankKeeper:    bankKeeper,
		commonsKeeper: commonsKeeper,
		seasonKeeper:  seasonKeeper,
		voteKeeper:    voteKeeper,
	}
}
