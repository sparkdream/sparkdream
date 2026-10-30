package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	guardiankeeper "sparkdream/x/guardian/keeper"
	guardiantypes "sparkdream/x/guardian/types"
)

// GRD-09: auth gas costs have ceilings; a cost no signed tx can pay would
// stop every tx, Technical Council votes included.
func TestAuthCeilings(t *testing.T) {
	with := func(f func(*authtypes.Params)) sdk.Msg {
		p := validAuthParams()
		f(&p)
		return &authtypes.MsgUpdateParams{Params: p}
	}
	runBoundCases(t, guardianDeps{}, []boundCase{
		{"genesis values", with(func(*authtypes.Params) {}), ""},
		{"per-byte at ceiling", with(func(p *authtypes.Params) { p.TxSizeCostPerByte = 100 }), ""},
		{"per-byte above ceiling", with(func(p *authtypes.Params) { p.TxSizeCostPerByte = 101 }), "tx_size_cost_per_byte above ceiling"},
		{"per-byte u64 max", with(func(p *authtypes.Params) { p.TxSizeCostPerByte = ^uint64(0) }), "tx_size_cost_per_byte above ceiling"},
		{"ed25519 above ceiling", with(func(p *authtypes.Params) { p.SigVerifyCostED25519 = 10_001 }), "sig_verify_cost_ed25519 above ceiling"},
		{"secp at ceiling", with(func(p *authtypes.Params) { p.SigVerifyCostSecp256k1 = 20_000 }), ""},
		{"secp 1e12", with(func(p *authtypes.Params) { p.SigVerifyCostSecp256k1 = 1_000_000_000_000 }), "sig_verify_cost_secp256k1 above ceiling"},
		{"sig limit above ceiling", with(func(p *authtypes.Params) { p.TxSigLimit = 101 }), "tx_sig_limit above ceiling"},
		{"memo above ceiling", with(func(p *authtypes.Params) { p.MaxMemoCharacters = 10_001 }), "max_memo_characters above ceiling"},
	})
}

// GRD-09: block.max_gas is finite and below a ceiling.
func TestConsensusMaxGasCeiling(t *testing.T) {
	with := func(gas int64) sdk.Msg {
		m := validConsensusMsg()
		m.Block.MaxGas = gas
		return m
	}
	runBoundCases(t, guardianDeps{staking: mockStakingKeeper{params: currentStakingParams()}}, []boundCase{
		{"at ceiling", with(1_000_000_000), ""},
		{"above ceiling", with(1_000_000_001), "block.max_gas"},
		{"int64 max", with(int64(^uint64(0) >> 1)), "block.max_gas"},
	})
}

// GRD-10: staking bounds are two-sided.
func TestStakingCeilings(t *testing.T) {
	cur := currentStakingParams()
	with := func(f func(*stakingtypes.Params)) sdk.Msg {
		p := cur
		f(&p)
		return &stakingtypes.MsgUpdateParams{Params: p}
	}
	runBoundCases(t, guardianDeps{staking: mockStakingKeeper{params: cur}}, []boundCase{
		{"unbonding 60d", with(func(p *stakingtypes.Params) { p.UnbondingTime = 60 * 24 * time.Hour }), ""},
		{"unbonding 50y", with(func(p *stakingtypes.Params) { p.UnbondingTime = 50 * 365 * 24 * time.Hour }), "unbonding_time above ceiling"},
		{"max_entries 1", with(func(p *stakingtypes.Params) { p.MaxEntries = 1 }), "max_entries below floor"},
		{"historical huge", with(func(p *stakingtypes.Params) { p.HistoricalEntries = 1_000_000 }), "historical_entries above ceiling"},
		{"commission 100%", with(func(p *stakingtypes.Params) { p.MinCommissionRate = math.LegacyOneDec() }), "min_commission_rate above ceiling"},
		{"commission 20%", with(func(p *stakingtypes.Params) { p.MinCommissionRate = math.LegacyNewDecWithPrec(2, 1) }), ""},
		{"commission omitted", with(func(p *stakingtypes.Params) { p.MinCommissionRate = math.LegacyDec{} }), "min_commission_rate is required"},
	})
}

// GRD-10: unbonding_time cannot drop below the current consensus evidence
// age (the SDK requirement, now checked from the staking side as well).
func TestStakingUnbondingVersusEvidenceAge(t *testing.T) {
	cur := currentStakingParams()
	ctx, k := setupGuardian(t, guardianDeps{staking: mockStakingKeeper{params: cur}, router: &okRouter{}})
	ctx = sdk.UnwrapSDKContext(ctx).WithConsensusParams(cmtproto.ConsensusParams{
		Evidence: &cmtproto.EvidenceParams{MaxAgeDuration: 21 * 24 * time.Hour},
	})
	ms := guardiankeeper.NewMsgServerImpl(k)
	p := cur
	p.UnbondingTime = 7 * 24 * time.Hour // above the floor, below the evidence age
	any, err := codectypes.NewAnyWithValue(&stakingtypes.MsgUpdateParams{Params: p})
	require.NoError(t, err)
	_, err = ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
	require.Contains(t, err.Error(), "evidence.max_age_duration")
}

// GRD-10: slashing ceilings.
func TestSlashingCeilings(t *testing.T) {
	with := func(f func(*slashingtypes.Params)) sdk.Msg {
		p := validSlashingParams()
		f(&p)
		return &slashingtypes.MsgUpdateParams{Params: p}
	}
	runBoundCases(t, guardianDeps{}, []boundCase{
		{"double sign 100%", with(func(p *slashingtypes.Params) { p.SlashFractionDoubleSign = math.LegacyOneDec() }), "slash_fraction_double_sign above ceiling"},
		{"downtime 10%", with(func(p *slashingtypes.Params) { p.SlashFractionDowntime = math.LegacyNewDecWithPrec(1, 1) }), "slash_fraction_downtime above ceiling"},
		{"min signed 100%", with(func(p *slashingtypes.Params) { p.MinSignedPerWindow = math.LegacyOneDec() }), "min_signed_per_window above ceiling"},
		{"jail a year", with(func(p *slashingtypes.Params) { p.DowntimeJailDuration = 365 * 24 * time.Hour }), "downtime_jail_duration above ceiling"},
	})
}

// GRD-12: an omitted decimal is refused with a typed error, not a panic.
func TestOmittedDecimalsRefused(t *testing.T) {
	mint := currentMintParams()
	mint.InflationMin = math.LegacyDec{}
	slash := validSlashingParams()
	slash.SlashFractionDoubleSign = math.LegacyDec{}
	distr := validDistrParams()
	distr.CommunityTax = math.LegacyDec{}
	runBoundCases(t, guardianDeps{mint: mockMintKeeper{params: currentMintParams()}}, []boundCase{
		{"mint inflation_min", &minttypes.MsgUpdateParams{Params: mint}, "inflation_min is required"},
		{"slashing fraction", &slashingtypes.MsgUpdateParams{Params: slash}, "slash_fraction_double_sign is required"},
		{"distribution community_tax", &distrtypes.MsgUpdateParams{Params: distr}, "community_tax is required"},
	})
}

// GRD-11: blocks_per_year changes at most once a week, so several messages
// in one proposal (or proposals ending together) cannot compound the ±50%
// step.
func TestBlocksPerYearCooldown(t *testing.T) {
	cur := currentMintParams()
	r := &okRouter{}
	ctx, k := setupGuardian(t, guardianDeps{mint: mockMintKeeper{params: cur}, router: r})
	sdkCtx := sdk.UnwrapSDKContext(ctx).WithBlockTime(time.Unix(1_800_000_000, 0))
	ms := guardiankeeper.NewMsgServerImpl(k)
	exec := func(ctx sdk.Context, bpy uint64) error {
		p := cur
		p.BlocksPerYear = bpy
		any, err := codectypes.NewAnyWithValue(&minttypes.MsgUpdateParams{Params: p})
		require.NoError(t, err)
		_, err = ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
		return err
	}
	require.NoError(t, exec(sdkCtx, cur.BlocksPerYear+cur.BlocksPerYear/2))
	err := exec(sdkCtx, cur.BlocksPerYear+cur.BlocksPerYear/4)
	require.ErrorIs(t, err, guardiantypes.ErrImmutableField, "a second change in the same proposal is refused")
	require.Contains(t, err.Error(), "at most once per")
	require.NoError(t, exec(sdkCtx, cur.BlocksPerYear), "an unchanged value is not a change")

	later := sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(7 * 24 * time.Hour))
	require.NoError(t, exec(later, cur.BlocksPerYear+cur.BlocksPerYear/4))
	require.Equal(t, 3, r.routed)
}
