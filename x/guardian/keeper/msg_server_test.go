package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	guardiankeeper "sparkdream/x/guardian/keeper"
	guardianmodule "sparkdream/x/guardian/module"
	guardiantypes "sparkdream/x/guardian/types"
	identitytypes "sparkdream/x/identity/types"
)

// --- mocks ------------------------------------------------------------------

// mockIdentityKeeper returns a fixed sealed identity (or an error) per test
// configuration.
type mockIdentityKeeper struct {
	sealed identitytypes.ChainIdentity
	err    error
}

func (m mockIdentityKeeper) GetSealedIdentity(_ context.Context) (identitytypes.ChainIdentity, error) {
	if m.err != nil {
		return identitytypes.ChainIdentity{}, m.err
	}
	return m.sealed, nil
}

func (m mockIdentityKeeper) IsIdentityKeeper() {}

// mockMintKeeper returns fixed mint params.
type mockMintKeeper struct {
	params minttypes.Params
}

func (m mockMintKeeper) GetParams(_ context.Context) (minttypes.Params, error) {
	return m.params, nil
}

// mockStakingKeeper returns fixed staking params.
type mockStakingKeeper struct {
	params stakingtypes.Params
}

func (m mockStakingKeeper) GetParams(_ context.Context) (stakingtypes.Params, error) {
	return m.params, nil
}

// mockDistrKeeper returns fixed distribution params.
type mockDistrKeeper struct{ params distrtypes.Params }

func (m mockDistrKeeper) GetParams(_ context.Context) (distrtypes.Params, error) {
	return m.params, nil
}

// mockGovKeeper returns fixed gov params.
type mockGovKeeper struct{ params govtypesv1.Params }

func (m mockGovKeeper) GetParams(_ context.Context) (govtypesv1.Params, error) {
	return m.params, nil
}

// mockSlashingKeeper returns fixed slashing params.
type mockSlashingKeeper struct{ params slashingtypes.Params }

func (m mockSlashingKeeper) GetParams(_ context.Context) (slashingtypes.Params, error) {
	return m.params, nil
}

// mockAuthKeeper returns fixed auth params.
type mockAuthKeeper struct{ params authtypes.Params }

func (m mockAuthKeeper) GetParams(_ context.Context) (authtypes.Params, error) {
	return m.params, nil
}

// mockMsgRouter records the inner msg routed to it. Always returns success
// with an empty response.
type mockMsgRouter struct {
	captured []any
}

func (r *mockMsgRouter) Handler(msg any) func(ctx any, m any) (any, error) {
	// Reflective signature; we'll use HandlerByTypeURL instead in tests.
	return nil
}

// We can't easily build a real MessageRouter for unit tests without
// pulling in the full BaseApp. The tests below exercise the filter logic
// in isolation by calling msg-server with a router that records msgs but
// returns errors; we verify the filter rejects BEFORE routing happens.

// guardianDeps groups the optional keeper wires for setupGuardian. Nil
// fields mean "don't wire this keeper", so filters that depend on the
// missing keeper will fail-closed (as designed).
type guardianDeps struct {
	identity guardiantypes.IdentityKeeper
	mint     guardiantypes.MintKeeper
	staking  guardiantypes.StakingKeeper
	distr    guardiantypes.DistrKeeper
	gov      guardiantypes.GovKeeper
	slashing guardiantypes.SlashingKeeper
	auth     guardiantypes.AuthKeeper
}

// --- shared fixture ---------------------------------------------------------

func setupGuardian(t *testing.T, deps guardianDeps) (context.Context, *guardiankeeper.Keeper) {
	t.Helper()
	encCfg := moduletestutil.MakeTestEncodingConfig(guardianmodule.AppModule{})
	// Register sdk.Msg implementations for the inner Any unpacking
	authtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	consensustypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	distrtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	govtypesv1.RegisterInterfaces(encCfg.InterfaceRegistry)
	minttypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	slashingtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	stakingtypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	storeKey := storetypes.NewKVStoreKey(guardiantypes.StoreKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	k := guardiankeeper.NewKeeper(
		runtime.NewKVStoreService(storeKey),
		encCfg.Codec,
		addresscodec.NewBech32Codec("sprkdrm"),
		nil, // msg router; not exercised in filter-only tests
		authority,
	)
	if deps.identity != nil {
		k.SetIdentityKeeper(deps.identity)
	}
	if deps.mint != nil {
		k.SetMintKeeper(deps.mint)
	}
	if deps.staking != nil {
		k.SetStakingKeeper(deps.staking)
	}
	if deps.distr != nil {
		k.SetDistrKeeper(deps.distr)
	}
	if deps.gov != nil {
		k.SetGovKeeper(deps.gov)
	}
	if deps.slashing != nil {
		k.SetSlashingKeeper(deps.slashing)
	}
	if deps.auth != nil {
		k.SetAuthKeeper(deps.auth)
	}
	return ctx, &k
}

// govAuth is the gov module address used as the MsgExec authority across
// all positive-path tests below.
func govAuth() string { return authtypes.NewModuleAddress(govtypes.ModuleName).String() }

func newValidSealed() identitytypes.ChainIdentity {
	return identitytypes.ChainIdentity{
		ChainHumanName:       "Phoenix",
		ChainTickerPrefix:    "PHX",
		BondDenom:            "upspk.phoenix",
		BondDisplaySymbol:    "PSPK",
		BondDisplayName:      "Phoenix Spark",
		BondDisplayDecimals:  6,
		DreamDenom:           "dream.phoenix",
		DreamDisplaySymbol:   "PDRM",
		DreamDisplayName:     "Phoenix Dream",
		DreamDisplayDecimals: 6,
		FoundedAt:            1735689600,
	}
}

func currentMintParams() minttypes.Params {
	return minttypes.Params{
		MintDenom:           "upspk.phoenix",
		InflationRateChange: math.LegacyNewDecWithPrec(13, 2),
		InflationMax:        math.LegacyNewDecWithPrec(5, 2),
		InflationMin:        math.LegacyNewDecWithPrec(2, 2),
		GoalBonded:          math.LegacyNewDecWithPrec(67, 2),
		BlocksPerYear:       6_311_520,
	}
}

func currentStakingParams() stakingtypes.Params {
	p := stakingtypes.DefaultParams()
	p.BondDenom = "upspk.phoenix"
	return p
}

// --- authority guard --------------------------------------------------------

func TestExecRejectsWrongAuthority(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	any, _ := codectypes.NewAnyWithValue(&banktypes.MsgSetSendEnabled{
		Authority: "ignored", // will be overwritten anyway, but we don't reach that
	})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{
		Authority: "sprkdrm1someoneelse",
		Inner:     any,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrUnauthorized)
}

// --- allowlist guard --------------------------------------------------------

func TestExecRejectsUnknownInnerMsg(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	// Use a msg type that's not in the guardian allowlist.
	any, _ := codectypes.NewAnyWithValue(&banktypes.MsgSend{})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Inner:     any,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrInnerMsgNotAllowed)
}

// --- mint inflation filter -------------------------------------------------

func TestExecRejectsMintInflationMinChange(t *testing.T) {
	cur := currentMintParams()
	ctx, k := setupGuardian(t, guardianDeps{mint: mockMintKeeper{params: cur}})
	ms := guardiankeeper.NewMsgServerImpl(k)

	proposed := cur
	proposed.InflationMin = math.LegacyNewDecWithPrec(3, 2) // change from 0.02 to 0.03

	any, _ := codectypes.NewAnyWithValue(&minttypes.MsgUpdateParams{
		Authority: "ignored",
		Params:    proposed,
	})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Inner:     any,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "inflation_min")
}

func TestExecRejectsMintInflationMaxChange(t *testing.T) {
	cur := currentMintParams()
	ctx, k := setupGuardian(t, guardianDeps{mint: mockMintKeeper{params: cur}})
	ms := guardiankeeper.NewMsgServerImpl(k)

	proposed := cur
	proposed.InflationMax = math.LegacyNewDecWithPrec(10, 2)

	any, _ := codectypes.NewAnyWithValue(&minttypes.MsgUpdateParams{Params: proposed})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Inner:     any,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "inflation_max")
}

func TestExecRejectsMintGoalBondedChange(t *testing.T) {
	cur := currentMintParams()
	ctx, k := setupGuardian(t, guardianDeps{mint: mockMintKeeper{params: cur}})
	ms := guardiankeeper.NewMsgServerImpl(k)

	proposed := cur
	proposed.GoalBonded = math.LegacyNewDecWithPrec(80, 2)

	any, _ := codectypes.NewAnyWithValue(&minttypes.MsgUpdateParams{Params: proposed})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Inner:     any,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "goal_bonded")
}

func TestExecRejectsMintInflationRateChangeChange(t *testing.T) {
	cur := currentMintParams()
	ctx, k := setupGuardian(t, guardianDeps{mint: mockMintKeeper{params: cur}})
	ms := guardiankeeper.NewMsgServerImpl(k)

	proposed := cur
	proposed.InflationRateChange = math.LegacyNewDecWithPrec(20, 2)

	any, _ := codectypes.NewAnyWithValue(&minttypes.MsgUpdateParams{Params: proposed})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Inner:     any,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "inflation_rate_change")
}

// --- staking bond_denom filter ---------------------------------------------

func TestExecRejectsStakingBondDenomChange(t *testing.T) {
	cur := currentStakingParams()
	ctx, k := setupGuardian(t, guardianDeps{staking: mockStakingKeeper{params: cur}})
	ms := guardiankeeper.NewMsgServerImpl(k)

	proposed := cur
	proposed.BondDenom = "uattacker.phoenix"

	any, _ := codectypes.NewAnyWithValue(&stakingtypes.MsgUpdateParams{Params: proposed})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Inner:     any,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "bond_denom")
}

// --- positive path (no field changes) --------------------------------------
//
// We can't easily exercise the routing path in a pure-unit test (it
// requires a real MessageRouter wired to bank/mint/staking msg servers).
// The tests above prove the filter REJECTS forbidden changes; an
// end-to-end test that gov submits a no-change MsgUpdateParams and the
// inner routing succeeds requires the full app fixture. Tracked in
// the implementation-decisions doc as "depends on full app harness".

// --- AllowedMsgTypes ------------------------------------------------------

func TestAllowedMsgTypesIncludesExpected(t *testing.T) {
	expected := []string{
		"/cosmos.auth.v1beta1.MsgUpdateParams",
		"/cosmos.bank.v1beta1.MsgSetSendEnabled",
		"/cosmos.bank.v1beta1.MsgUpdateParams",
		"/cosmos.consensus.v1.MsgUpdateParams",
		"/cosmos.distribution.v1beta1.MsgCommunityPoolSpend",
		"/cosmos.distribution.v1beta1.MsgUpdateParams",
		"/cosmos.gov.v1.MsgUpdateParams",
		"/cosmos.mint.v1beta1.MsgUpdateParams",
		"/cosmos.slashing.v1beta1.MsgUpdateParams",
		"/cosmos.staking.v1beta1.MsgUpdateParams",
	}
	got := guardiankeeper.AllowedMsgTypes()
	require.ElementsMatch(t, expected, got)
}

func TestAllowedMsgTypesExcludesUnsafe(t *testing.T) {
	got := guardiankeeper.AllowedMsgTypes()
	// MsgSend is not authority-gated and must not be exposed via guardian.
	for _, typeURL := range got {
		require.NotContains(t, typeURL, "MsgSend")
	}
}

// --- ModuleAddress ---------------------------------------------------------

func TestModuleAddressStable(t *testing.T) {
	addr1 := guardiankeeper.ModuleAddress()
	addr2 := guardiankeeper.ModuleAddress()
	require.Equal(t, addr1, addr2, "ModuleAddress must be deterministic")
	require.NotEmpty(t, addr1)
}

// --- mint_denom filter -----------------------------------------------------
//
// Parity with the staking bond_denom filter: both denoms are sealed at
// genesis, so any change is a hard reject regardless of authority.

func TestExecRejectsMintDenomChange(t *testing.T) {
	cur := currentMintParams()
	ctx, k := setupGuardian(t, guardianDeps{mint: mockMintKeeper{params: cur}})
	ms := guardiankeeper.NewMsgServerImpl(k)

	proposed := cur
	proposed.MintDenom = "uattacker.phoenix"

	any, _ := codectypes.NewAnyWithValue(&minttypes.MsgUpdateParams{Params: proposed})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "mint_denom")
}

// --- bank SetSendEnabled native-denom filter -------------------------------

func TestExecRejectsBankDisableNativeBond(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{
		identity: mockIdentityKeeper{sealed: newValidSealed()},
	})
	ms := guardiankeeper.NewMsgServerImpl(k)

	msg := &banktypes.MsgSetSendEnabled{
		SendEnabled: []*banktypes.SendEnabled{{Denom: "upspk.phoenix", Enabled: false}},
	}
	any, _ := codectypes.NewAnyWithValue(msg)
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "upspk.phoenix")
}

func TestExecRejectsBankDisableNativeDream(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{
		identity: mockIdentityKeeper{sealed: newValidSealed()},
	})
	ms := guardiankeeper.NewMsgServerImpl(k)

	msg := &banktypes.MsgSetSendEnabled{
		SendEnabled: []*banktypes.SendEnabled{{Denom: "dream.phoenix", Enabled: false}},
	}
	any, _ := codectypes.NewAnyWithValue(msg)
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "dream.phoenix")
}

func TestExecRejectsBankUseDefaultForNativeDenom(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{
		identity: mockIdentityKeeper{sealed: newValidSealed()},
	})
	ms := guardiankeeper.NewMsgServerImpl(k)

	msg := &banktypes.MsgSetSendEnabled{
		UseDefaultFor: []string{"upspk.phoenix"},
	}
	any, _ := codectypes.NewAnyWithValue(msg)
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
}

func TestExecRejectsBankSetSendEnabledIfIdentityUnwired(t *testing.T) {
	// Fail-closed: without identity wired, the filter cannot tell native
	// from foreign denoms, so every SetSendEnabled is rejected.
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	any, _ := codectypes.NewAnyWithValue(&banktypes.MsgSetSendEnabled{
		SendEnabled: []*banktypes.SendEnabled{{Denom: "ibc/ABC", Enabled: true}},
	})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrIdentityNotSealed)
}

// --- distribution.MsgCommunityPoolSpend hard reject ------------------------

func TestExecRejectsCommunityPoolSpendOutright(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	any, _ := codectypes.NewAnyWithValue(&distrtypes.MsgCommunityPoolSpend{
		Recipient: "sprkdrm1somerecipient",
	})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrInnerMsgNotAllowed)
	require.Contains(t, err.Error(), "x/split")
}

// --- distribution.MsgUpdateParams community_tax bounds ---------------------

func validDistrParams() distrtypes.Params {
	return distrtypes.Params{
		CommunityTax:        math.LegacyNewDecWithPrec(15, 2),
		BaseProposerReward:  math.LegacyZeroDec(),
		BonusProposerReward: math.LegacyZeroDec(),
		WithdrawAddrEnabled: true,
	}
}

func TestExecRejectsCommunityTaxBelowFloor(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{distr: mockDistrKeeper{params: validDistrParams()}})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validDistrParams()
	p.CommunityTax = math.LegacyNewDecWithPrec(1, 2) // 1% — below 5% floor
	any, _ := codectypes.NewAnyWithValue(&distrtypes.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "community_tax")
}

func TestExecRejectsCommunityTaxAboveCeiling(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{distr: mockDistrKeeper{params: validDistrParams()}})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validDistrParams()
	p.CommunityTax = math.LegacyNewDecWithPrec(50, 2) // 50% — above 25% ceiling
	any, _ := codectypes.NewAnyWithValue(&distrtypes.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "community_tax")
}

// --- gov.MsgUpdateParams floors --------------------------------------------

func validGovParams() govtypesv1.Params {
	vp := 2 * 24 * time.Hour
	evp := 24 * time.Hour
	return govtypesv1.Params{
		VotingPeriod:           &vp,
		ExpeditedVotingPeriod:  &evp,
		Quorum:                 "0.334",
		Threshold:              "0.5",
		VetoThreshold:          "0.334",
		MinInitialDepositRatio: "0",
		ProposalCancelRatio:    "0",
		MinDepositRatio:        "0",
		ExpeditedThreshold:     "0.667",
	}
}

func TestExecRejectsShortVotingPeriod(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validGovParams()
	short := 1 * time.Hour
	p.VotingPeriod = &short
	any, _ := codectypes.NewAnyWithValue(&govtypesv1.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "voting_period")
}

func TestExecRejectsLowQuorum(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validGovParams()
	p.Quorum = "0.05"
	any, _ := codectypes.NewAnyWithValue(&govtypesv1.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "quorum")
}

func TestExecRejectsLowThreshold(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validGovParams()
	p.Threshold = "0.3"
	any, _ := codectypes.NewAnyWithValue(&govtypesv1.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "threshold")
}

func TestExecRejectsLowVetoThreshold(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validGovParams()
	p.VetoThreshold = "0.05"
	any, _ := codectypes.NewAnyWithValue(&govtypesv1.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "veto_threshold")
}

// --- slashing.MsgUpdateParams floors ---------------------------------------

func validSlashingParams() slashingtypes.Params {
	return slashingtypes.Params{
		SignedBlocksWindow:      100,
		MinSignedPerWindow:      math.LegacyNewDecWithPrec(5, 1),
		DowntimeJailDuration:    10 * time.Minute,
		SlashFractionDoubleSign: math.LegacyNewDecWithPrec(5, 2),
		SlashFractionDowntime:   math.LegacyNewDecWithPrec(1, 4),
	}
}

func TestExecRejectsZeroSlashDoubleSign(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validSlashingParams()
	p.SlashFractionDoubleSign = math.LegacyZeroDec()
	any, _ := codectypes.NewAnyWithValue(&slashingtypes.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "slash_fraction_double_sign")
}

func TestExecRejectsZeroSlashDowntime(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validSlashingParams()
	p.SlashFractionDowntime = math.LegacyZeroDec()
	any, _ := codectypes.NewAnyWithValue(&slashingtypes.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "slash_fraction_downtime")
}

// --- auth.MsgUpdateParams floors -------------------------------------------

func validAuthParams() authtypes.Params {
	return authtypes.Params{
		MaxMemoCharacters:      256,
		TxSigLimit:             7,
		TxSizeCostPerByte:      10,
		SigVerifyCostED25519:   590,
		SigVerifyCostSecp256k1: 1000,
	}
}

func TestExecRejectsZeroTxSizeCost(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validAuthParams()
	p.TxSizeCostPerByte = 0
	any, _ := codectypes.NewAnyWithValue(&authtypes.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "tx_size_cost_per_byte")
}

func TestExecRejectsZeroSigVerifyEd(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validAuthParams()
	p.SigVerifyCostED25519 = 0
	any, _ := codectypes.NewAnyWithValue(&authtypes.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "sig_verify_cost_ed25519")
}

// --- consensus.MsgUpdateParams floors --------------------------------------

func validConsensusMsg() *consensustypes.MsgUpdateParams {
	return &consensustypes.MsgUpdateParams{
		Block: &cmtproto.BlockParams{
			MaxBytes: 22_020_096,
			MaxGas:   -1,
		},
		Evidence: &cmtproto.EvidenceParams{
			MaxAgeNumBlocks: 100_000,
			MaxAgeDuration:  48 * time.Hour,
			MaxBytes:        1_048_576,
		},
		Validator: &cmtproto.ValidatorParams{
			PubKeyTypes: []string{"ed25519"},
		},
	}
}

func TestExecRejectsTinyMaxBytes(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	m := validConsensusMsg()
	m.Block.MaxBytes = 1000 // below 200_000 floor
	any, _ := codectypes.NewAnyWithValue(m)
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "block.max_bytes")
}

func TestExecRejectsTinyMaxGas(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	m := validConsensusMsg()
	m.Block.MaxGas = 1000 // not -1 and below 1_000_000 floor
	any, _ := codectypes.NewAnyWithValue(m)
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "block.max_gas")
}

func TestExecRejectsShortEvidenceAge(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	m := validConsensusMsg()
	m.Evidence.MaxAgeNumBlocks = 100 // below 1000 floor
	any, _ := codectypes.NewAnyWithValue(m)
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "max_age_num_blocks")
}
