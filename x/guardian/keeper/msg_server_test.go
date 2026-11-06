package keeper_test

import (
	"context"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
	router   guardiantypes.MessageRouter
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
		deps.router, // nil in filter-only tests; okRouter for accept paths
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
	mdp := 2 * 24 * time.Hour
	return govtypesv1.Params{
		VotingPeriod:           &vp,
		ExpeditedVotingPeriod:  &evp,
		MaxDepositPeriod:       &mdp,
		Quorum:                 "0.334",
		Threshold:              "0.5",
		VetoThreshold:          "0.334",
		MinInitialDepositRatio: "0",
		ProposalCancelRatio:    "0",
		MinDepositRatio:        "0.01",
		ExpeditedThreshold:     "0.667",
		MinDeposit:             sdk.NewCoins(sdk.NewInt64Coin("upspk.phoenix", 50_000_000)),
		ExpeditedMinDeposit:    sdk.NewCoins(sdk.NewInt64Coin("upspk.phoenix", 250_000_000)),
	}
}

func TestExecRejectsShortVotingPeriod(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validGovParams()
	// Just under the build's floor (per network, see network_bounds.go).
	short := k.NetworkBounds().GovVotingPeriodFloor - time.Minute
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

func TestExecRejectsLowTxSigLimit(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := validAuthParams()
	p.TxSigLimit = 6 // a 7-key multisig would no longer be able to sign
	any, _ := codectypes.NewAnyWithValue(&authtypes.MsgUpdateParams{Params: p})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.Error(t, err)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "tx_sig_limit")
}

// --- consensus.MsgUpdateParams floors --------------------------------------

func validConsensusMsg() *consensustypes.MsgUpdateParams {
	return &consensustypes.MsgUpdateParams{
		Block: &cmtproto.BlockParams{
			MaxBytes: 22_020_096,
			MaxGas:   100_000_000,
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

// --- accept/reject tables for the GRD-01/02/03 bounds ----------------------

// okRouter routes every msg to a handler that succeeds, so a filter that
// accepts can be told apart from one that rejects.
type okRouter struct{ routed int }

func (r *okRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler {
	return func(sdk.Context, sdk.Msg) (*sdk.Result, error) {
		r.routed++
		return &sdk.Result{}, nil
	}
}

func (r *okRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return r.Handler(nil) }

type boundCase struct {
	name    string
	inner   sdk.Msg
	wantErr string // "" = accepted and routed
}

func runBoundCases(t *testing.T, deps guardianDeps, cases []boundCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &okRouter{}
			d := deps
			d.router = r
			ctx, k := setupGuardian(t, d)
			ms := guardiankeeper.NewMsgServerImpl(k)
			any, err := codectypes.NewAnyWithValue(tc.inner)
			require.NoError(t, err)
			_, err = ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
			if tc.wantErr == "" {
				require.NoError(t, err)
				require.Equal(t, 1, r.routed)
				return
			}
			require.Error(t, err)
			require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
			require.Contains(t, err.Error(), tc.wantErr)
			require.Zero(t, r.routed)
		})
	}
}

// GRD-01: blocks_per_year divides per-block issuance; it must stay inside
// the compiled range and move at most 50% per proposal.
func TestMintBlocksPerYearBounds(t *testing.T) {
	cur := currentMintParams() // 6_311_520
	with := func(bpy uint64) sdk.Msg {
		p := cur
		p.BlocksPerYear = bpy
		return &minttypes.MsgUpdateParams{Params: p}
	}
	runBoundCases(t, guardianDeps{mint: mockMintKeeper{params: cur}}, []boundCase{
		{"unchanged", with(6_311_520), ""},
		{"small bump", with(6_311_521), ""},
		{"+50% exactly", with(9_467_280), ""},
		{"-50% exactly", with(3_155_760), ""},
		{"one mints a year per block", with(1), "blocks_per_year outside"},
		{"below absolute floor", with(3_000_000), "blocks_per_year outside"},
		{"above absolute ceiling", with(100_000_000), "blocks_per_year outside"},
		{"u64 max", with(^uint64(0)), "blocks_per_year outside"},
		{"more than +50%", with(9_467_281), "at most 50%"},
		{"more than -50%", with(3_155_759), "at most 50%"},
	})
}

// GRD-02: default_send_enabled=false would disable every native send.
func TestBankUpdateParamsBounds(t *testing.T) {
	runBoundCases(t, guardianDeps{}, []boundCase{
		{"default true", &banktypes.MsgUpdateParams{Params: banktypes.Params{DefaultSendEnabled: true}}, ""},
		{"default false", &banktypes.MsgUpdateParams{Params: banktypes.Params{DefaultSendEnabled: false}}, "default_send_enabled"},
		{"legacy entry disables", &banktypes.MsgUpdateParams{Params: banktypes.Params{
			DefaultSendEnabled: true,
			SendEnabled:        []*banktypes.SendEnabled{{Denom: "upspk.phoenix", Enabled: false}},
		}}, "legacy send_enabled"},
	})
}

// GRD-03: slashing floors on downtime enforcement.
func TestSlashingDowntimeBounds(t *testing.T) {
	with := func(f func(*slashingtypes.Params)) sdk.Msg {
		p := validSlashingParams()
		f(&p)
		return &slashingtypes.MsgUpdateParams{Params: p}
	}
	runBoundCases(t, guardianDeps{}, []boundCase{
		{"genesis values", with(func(*slashingtypes.Params) {}), ""},
		{"jail bump", with(func(p *slashingtypes.Params) { p.DowntimeJailDuration += time.Second }), ""},
		{"min_signed zero", with(func(p *slashingtypes.Params) { p.MinSignedPerWindow = math.LegacyZeroDec() }), "min_signed_per_window"},
		{"min_signed just under", with(func(p *slashingtypes.Params) { p.MinSignedPerWindow = math.LegacyNewDecWithPrec(4, 2) }), "min_signed_per_window"},
		{"jail near zero", with(func(p *slashingtypes.Params) { p.DowntimeJailDuration = time.Nanosecond }), "downtime_jail_duration"},
		{"window tiny", with(func(p *slashingtypes.Params) { p.SignedBlocksWindow = 1 }), "signed_blocks_window below"},
		{"window huge", with(func(p *slashingtypes.Params) { p.SignedBlocksWindow = 2_000_000 }), "signed_blocks_window above"},
	})
}

// GRD-03: staking floors on unbonding_time and max_validators.
func TestStakingBounds(t *testing.T) {
	cur := currentStakingParams()
	with := func(f func(*stakingtypes.Params)) sdk.Msg {
		p := cur
		f(&p)
		return &stakingtypes.MsgUpdateParams{Params: p}
	}
	runBoundCases(t, guardianDeps{staking: mockStakingKeeper{params: cur}}, []boundCase{
		{"current", with(func(*stakingtypes.Params) {}), ""},
		{"max_validators bump", with(func(p *stakingtypes.Params) { p.MaxValidators++ }), ""},
		{"unbonding 1ns", with(func(p *stakingtypes.Params) { p.UnbondingTime = time.Nanosecond }), "unbonding_time"},
		{"unbonding 6d", with(func(p *stakingtypes.Params) { p.UnbondingTime = 6 * 24 * time.Hour }), "unbonding_time"},
		{"one validator", with(func(p *stakingtypes.Params) { p.MaxValidators = 1 }), "max_validators"},
	})
}

// GRD-03: consensus pub_key_types must keep ed25519, max_gas must be
// finite, evidence must not outlive the unbonding period.
func TestConsensusBounds(t *testing.T) {
	with := func(f func(*consensustypes.MsgUpdateParams)) sdk.Msg {
		m := validConsensusMsg()
		f(m)
		return m
	}
	runBoundCases(t, guardianDeps{staking: mockStakingKeeper{params: currentStakingParams()}}, []boundCase{
		{"valid", with(func(*consensustypes.MsgUpdateParams) {}), ""},
		{"ed25519 plus secp", with(func(m *consensustypes.MsgUpdateParams) {
			m.Validator.PubKeyTypes = []string{"ed25519", "secp256k1"}
		}), ""},
		{"secp only halts", with(func(m *consensustypes.MsgUpdateParams) {
			m.Validator.PubKeyTypes = []string{"secp256k1"}
		}), "pub_key_types"},
		{"empty key types", with(func(m *consensustypes.MsgUpdateParams) { m.Validator.PubKeyTypes = nil }), "pub_key_types"},
		{"unlimited gas", with(func(m *consensustypes.MsgUpdateParams) { m.Block.MaxGas = -1 }), "block.max_gas"},
		{"zero gas", with(func(m *consensustypes.MsgUpdateParams) { m.Block.MaxGas = 0 }), "block.max_gas"},
		{"evidence outlives unbonding", with(func(m *consensustypes.MsgUpdateParams) {
			m.Evidence.MaxAgeDuration = 30 * 24 * time.Hour
		}), "exceeds staking unbonding_time"},
	})
}

func TestConsensusFailsClosedWithoutStaking(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{router: &okRouter{}})
	ms := guardiankeeper.NewMsgServerImpl(k)
	any, _ := codectypes.NewAnyWithValue(validConsensusMsg())
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "staking keeper not wired")
}

// GRD-03: gov voting-period ceilings and a positive native deposit.
func TestGovBounds(t *testing.T) {
	with := func(f func(*govtypesv1.Params)) sdk.Msg {
		p := validGovParams()
		f(&p)
		return &govtypesv1.MsgUpdateParams{Params: p}
	}
	decades := 20 * 365 * 24 * time.Hour
	runBoundCases(t, guardianDeps{identity: mockIdentityKeeper{sealed: newValidSealed()}}, []boundCase{
		{"valid", with(func(*govtypesv1.Params) {}), ""},
		{"voting period decades", with(func(p *govtypesv1.Params) { p.VotingPeriod = &decades }), "voting_period above ceiling"},
		{"expedited decades", with(func(p *govtypesv1.Params) { p.ExpeditedVotingPeriod = &decades }), "expedited_voting_period above ceiling"},
		{"empty min_deposit", with(func(p *govtypesv1.Params) { p.MinDeposit = nil }), "min_deposit"},
		{"foreign-only min_deposit", with(func(p *govtypesv1.Params) {
			p.MinDeposit = sdk.NewCoins(sdk.NewInt64Coin("ibc/zenith", 1))
		}), "min_deposit"},
		{"empty expedited_min_deposit", with(func(p *govtypesv1.Params) { p.ExpeditedMinDeposit = nil }), "expedited_min_deposit"},
		// GRD-07: min_deposit_ratio is parsed by every submission and deposit.
		{"empty min_deposit_ratio", with(func(p *govtypesv1.Params) { p.MinDepositRatio = "" }), "min_deposit_ratio"},
		{"garbage min_deposit_ratio", with(func(p *govtypesv1.Params) { p.MinDepositRatio = "abc" }), "min_deposit_ratio"},
		{"negative min_deposit_ratio", with(func(p *govtypesv1.Params) { p.MinDepositRatio = "-0.1" }), "min_deposit_ratio"},
		{"min_deposit_ratio above 1", with(func(p *govtypesv1.Params) { p.MinDepositRatio = "1.5" }), "min_deposit_ratio"},
		{"min_deposit_ratio at 1", with(func(p *govtypesv1.Params) { p.MinDepositRatio = "1" }), ""},
		{"min_initial_deposit_ratio above 1", with(func(p *govtypesv1.Params) { p.MinInitialDepositRatio = "2" }), "min_initial_deposit_ratio"},
		// GRD-08: ceilings and the expedited floor stop a self-freeze.
		{"quorum 1", with(func(p *govtypesv1.Params) { p.Quorum = "1" }), "quorum above ceiling"},
		{"quorum at ceiling", with(func(p *govtypesv1.Params) { p.Quorum = "0.5" }), ""},
		{"threshold near 1", with(func(p *govtypesv1.Params) { p.Threshold = "0.999"; p.ExpeditedThreshold = "1" }), "threshold above ceiling"},
		{"veto disabled", with(func(p *govtypesv1.Params) { p.VetoThreshold = "1" }), "veto_threshold above ceiling"},
		{"weak expedited", with(func(p *govtypesv1.Params) { p.ExpeditedThreshold = "0.51" }), "expedited_threshold below floor"},
		{"dust min_deposit", with(func(p *govtypesv1.Params) {
			p.MinDeposit = sdk.NewCoins(sdk.NewInt64Coin("upspk.phoenix", 1))
		}), "min_deposit"},
		{"expedited below min_deposit", with(func(p *govtypesv1.Params) {
			p.ExpeditedMinDeposit = sdk.NewCoins(sdk.NewInt64Coin("upspk.phoenix", 10_000_000))
		}), "expedited_min_deposit"},
		{"deposit period decades", with(func(p *govtypesv1.Params) { p.MaxDepositPeriod = &decades }), "max_deposit_period"},
		{"no deposit period", with(func(p *govtypesv1.Params) { p.MaxDepositPeriod = nil }), "max_deposit_period"},
	})
}

func TestGovFailsClosedWithoutIdentity(t *testing.T) {
	ctx, k := setupGuardian(t, guardianDeps{router: &okRouter{}})
	ms := guardiankeeper.NewMsgServerImpl(k)
	any, _ := codectypes.NewAnyWithValue(&govtypesv1.MsgUpdateParams{Params: validGovParams()})
	_, err := ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.ErrorIs(t, err, guardiantypes.ErrIdentityNotSealed)
}

// eventRouter's handler emits one event, as a real msg server would.
type eventRouter struct{}

func (eventRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler {
	return func(sdk.Context, sdk.Msg) (*sdk.Result, error) {
		return &sdk.Result{Events: []abci.Event{{Type: "params_updated"}}}, nil
	}
}

func (r eventRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return r.Handler(nil) }

// SES-06: the inner handler's events reach the block (the router gives the
// handler a fresh EventManager).
func TestExecReemitsInnerEvents(t *testing.T) {
	goCtx, k := setupGuardian(t, guardianDeps{router: eventRouter{}})
	ctx := sdk.UnwrapSDKContext(goCtx).WithEventManager(sdk.NewEventManager())
	ms := guardiankeeper.NewMsgServerImpl(k)
	any, err := codectypes.NewAnyWithValue(&banktypes.MsgUpdateParams{Params: banktypes.Params{DefaultSendEnabled: true}})
	require.NoError(t, err)
	_, err = ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.NoError(t, err)
	found := false
	for _, e := range ctx.EventManager().Events() {
		if e.Type == "params_updated" {
			found = true
		}
	}
	require.True(t, found)
}
