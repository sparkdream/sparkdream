package keeper_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Automatic funding exists so bonded-role pay does not depend on a council
// remembering to send SPARK. These tests pin the two properties that make the
// automatic version safe to leave running unattended: it cannot draw more than
// the daily cap no matter how many blocks pass, and it cannot draw more than
// the pools can actually absorb.

// mockDistrKeeper is an in-memory community pool.
type mockDistrKeeper struct {
	tax    math.LegacyDec
	pool   math.Int
	denom  string
	ledger map[string]math.Int
	failOn bool
}

func (m *mockDistrKeeper) GetCommunityPool(_ context.Context) (sdk.DecCoins, error) {
	return sdk.NewDecCoins(sdk.NewDecCoin(m.denom, m.pool)), nil
}

// GetCommunityTax is held at 1 in these tests so the daily allowance is exactly
// provisions x share -- the arithmetic under test is the split and the cap, not
// the SDK's tax fraction.
func (m *mockDistrKeeper) GetCommunityTax(_ context.Context) (math.LegacyDec, error) {
	if m.tax.IsNil() {
		return math.LegacyOneDec(), nil
	}
	return m.tax, nil
}

// mockMintKeeper supplies annual provisions, the base the daily allowance is a
// share of.
type mockMintKeeper struct {
	provisions math.LegacyDec
}

func (m *mockMintKeeper) AnnualProvisions(_ context.Context) (math.LegacyDec, error) {
	if m.provisions.IsNil() {
		return math.LegacyZeroDec(), nil
	}
	return m.provisions, nil
}

func (m *mockDistrKeeper) DistributeFromFeePool(_ context.Context, amount sdk.Coins, to sdk.AccAddress) error {
	if m.failOn {
		return fmt.Errorf("distribution unavailable")
	}
	v := amount.AmountOf(m.denom)
	if m.pool.LT(v) {
		return fmt.Errorf("insufficient community pool")
	}
	m.pool = m.pool.Sub(v)
	cur, ok := m.ledger[to.String()]
	if !ok {
		cur = math.ZeroInt()
	}
	m.ledger[to.String()] = cur.Add(v)
	return nil
}

// installFunding wires an in-memory bank ledger and community pool together so
// a skim can be observed end to end.
func installFunding(t *testing.T, f *fixture, poolBalance math.Int) (map[string]math.Int, *mockDistrKeeper, *mockMintKeeper) {
	t.Helper()
	ledger := installLedger(f)
	denom := f.keeper.BondDenom(f.ctx)
	dk := &mockDistrKeeper{pool: poolBalance, denom: denom, ledger: ledger, tax: math.LegacyOneDec()}
	mk := &mockMintKeeper{provisions: math.LegacyZeroDec()}
	f.keeper.SetDistrKeeper(dk)
	f.keeper.SetMintKeeper(mk)

	// Pin the share at 1 and the tax at 1 so setDailyAllowance below can name an
	// exact SPARK/day figure. What these tests exercise is the division, the
	// day ledger and the clamps -- the share arithmetic has its own test.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.RoleRewardInflationShare = math.LegacyOneDec()
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	return ledger, dk, mk
}

// setDailyAllowance makes roleRewardDailyAllowance return exactly `amount` by
// working backwards through provisions * tax * share / 365, with tax and share
// pinned at 1 by installFunding.
func setDailyAllowance(mk *mockMintKeeper, amount int64) {
	mk.provisions = math.LegacyNewDec(amount).MulInt64(365)
}

func balanceOf(ledger map[string]math.Int, addr sdk.AccAddress) math.Int {
	if v, ok := ledger[addr.String()]; ok {
		return v
	}
	return math.ZeroInt()
}

func TestFundRoleRewardPoolsSplitsByHeadroom(t *testing.T) {
	f := initFixture(t)
	ledger, dk, mk := installFunding(t, f, math.NewInt(1_000_000_000_000))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	// Give the sentinel pool three times the reviewer pool's headroom.
	params.MaxSentinelRewardPool = math.NewInt(3_000_000)
	params.MaxReviewerRewardPool = math.NewInt(1_000_000)
	params.MaxCuratorRewardPool = math.ZeroInt()  // a zero-cap pool is skipped entirely
	params.MaxVerifierRewardPool = math.ZeroInt() // ditto
	setDailyAllowance(mk, 400_000)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))

	sentinel := balanceOf(ledger, keeper.SentinelRewardPoolAddress())
	reviewer := balanceOf(ledger, keeper.ReviewerRewardPoolAddress())
	require.Equal(t, math.NewInt(300_000), sentinel, "3:1 headroom should take 3:1 of the draw")
	require.Equal(t, math.NewInt(100_000), reviewer)
	require.Equal(t, math.NewInt(400_000), sentinel.Add(reviewer), "the whole draw must be placed")
	require.True(t, balanceOf(ledger, keeper.RoleRewardIntakeAddress()).IsZero(),
		"intake is a conduit, not a holding account")
	require.Equal(t, math.NewInt(1_000_000_000_000-400_000), dk.pool)
}

func TestFundRoleRewardPoolsRespectsDailyCapAcrossBlocks(t *testing.T) {
	f := initFixture(t)
	ledger, _, mk := installFunding(t, f, math.NewInt(1_000_000_000_000))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxSentinelRewardPool = math.NewInt(100_000_000)
	params.MaxReviewerRewardPool = math.NewInt(100_000_000)
	params.MaxCuratorRewardPool = math.NewInt(100_000_000)
	params.MaxVerifierRewardPool = math.NewInt(100_000_000)
	setDailyAllowance(mk, 500_000)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Headroom vastly exceeds the cap, so without the ledger every block would
	// draw another full cap. Run enough blocks to make that obvious.
	for i := 0; i < 20; i++ {
		require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))
	}
	poolTotal := func() math.Int {
		return balanceOf(ledger, keeper.SentinelRewardPoolAddress()).
			Add(balanceOf(ledger, keeper.ReviewerRewardPoolAddress())).
			Add(balanceOf(ledger, keeper.CuratorRewardPoolAddress())).
			Add(balanceOf(ledger, keeper.VerifierRewardPoolAddress()))
	}
	require.Equal(t, math.NewInt(500_000), poolTotal(), "20 blocks in one UTC day must draw one cap")

	// A new UTC day releases exactly one more cap.
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(25 * time.Hour))
	require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))
	require.Equal(t, math.NewInt(1_000_000), poolTotal())
}

func TestFundRoleRewardPoolsStopsAtPoolCaps(t *testing.T) {
	f := initFixture(t)
	ledger, dk, mk := installFunding(t, f, math.NewInt(1_000_000_000_000))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxSentinelRewardPool = math.NewInt(10_000)
	params.MaxReviewerRewardPool = math.NewInt(10_000)
	params.MaxCuratorRewardPool = math.NewInt(10_000)
	params.MaxVerifierRewardPool = math.NewInt(10_000)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	setDailyAllowance(mk, 1_000_000_000) // far above total headroom

	require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))
	require.Equal(t, math.NewInt(10_000), balanceOf(ledger, keeper.SentinelRewardPoolAddress()))
	require.Equal(t, math.NewInt(10_000), balanceOf(ledger, keeper.ReviewerRewardPoolAddress()))
	require.Equal(t, math.NewInt(10_000), balanceOf(ledger, keeper.CuratorRewardPoolAddress()))
	require.Equal(t, math.NewInt(10_000), balanceOf(ledger, keeper.VerifierRewardPoolAddress()))
	require.Equal(t, math.NewInt(1_000_000_000_000-40_000), dk.pool,
		"the draw is bounded by total headroom, not by the daily cap")

	// Every pool is now full: a further block must not touch the community pool.
	before := dk.pool
	require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))
	require.Equal(t, before, dk.pool, "an idle role costs the community pool nothing")
}

func TestFundRoleRewardPoolsBoundedByCommunityPool(t *testing.T) {
	f := initFixture(t)
	ledger, dk, mk := installFunding(t, f, math.NewInt(7_000))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxSentinelRewardPool = math.NewInt(1_000_000)
	params.MaxReviewerRewardPool = math.NewInt(1_000_000)
	params.MaxCuratorRewardPool = math.NewInt(1_000_000)
	params.MaxVerifierRewardPool = math.NewInt(1_000_000)
	setDailyAllowance(mk, 1_000_000)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// The community pool holds less than the cap; drawing the cap would fail
	// DistributeFromFeePool and, in BeginBlock, take the block with it.
	require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))
	total := balanceOf(ledger, keeper.SentinelRewardPoolAddress()).
		Add(balanceOf(ledger, keeper.ReviewerRewardPoolAddress())).
		Add(balanceOf(ledger, keeper.CuratorRewardPoolAddress())).
		Add(balanceOf(ledger, keeper.VerifierRewardPoolAddress()))
	require.Equal(t, math.NewInt(7_000), total, "the whole available pool is drawn and placed")
	require.True(t, dk.pool.IsZero())
}

func TestFundRoleRewardPoolsDisabledAtZero(t *testing.T) {
	f := initFixture(t)
	ledger, dk, mk := installFunding(t, f, math.NewInt(1_000_000_000))
	setDailyAllowance(mk, 500_000) // provisions are healthy...

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.RoleRewardInflationShare = math.LegacyZeroDec() // ...but the share is off
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))
	require.Equal(t, math.NewInt(1_000_000_000), dk.pool, "zero must disable the skim, not mean unlimited")
	require.True(t, balanceOf(ledger, keeper.SentinelRewardPoolAddress()).IsZero())
	require.True(t, balanceOf(ledger, keeper.ReviewerRewardPoolAddress()).IsZero())
}

func TestFundRoleRewardPoolsSurvivesDistributionFailure(t *testing.T) {
	f := initFixture(t)
	_, dk, mk := installFunding(t, f, math.NewInt(1_000_000_000))
	dk.failOn = true

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	setDailyAllowance(mk, 500_000)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// BeginBlock must not halt the chain over a funding failure, and a failed
	// draw must not consume the day's budget.
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	day := uint64(f.ctx.BlockTime().Unix()) / 86400
	require.True(t, f.keeper.GetRoleRewardDayFunding(f.ctx, day).IsZero(),
		"a failed draw must not burn the day's allowance")
}

func TestFundRoleRewardPoolsNoopWithoutDistrKeeper(t *testing.T) {
	f := initFixture(t)
	// The keeper is late-wired; an unwired chain (or an older test fixture)
	// must simply not fund rather than nil-panic in BeginBlock.
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
}

func TestRoleRewardInflationShareParamValidation(t *testing.T) {
	p := types.DefaultParams()
	require.True(t, p.RoleRewardInflationShare.IsPositive())
	require.NoError(t, p.Validate())

	p.RoleRewardInflationShare = math.LegacyZeroDec()
	require.NoError(t, p.Validate(), "zero is a valid way to disable automatic funding")

	p.RoleRewardInflationShare = math.LegacyNewDec(-1)
	require.Error(t, p.Validate())

	// Above 1 would mean claiming more than the community pool's whole
	// inflation income -- i.e. intending to leave the councils nothing. That
	// should not be expressible, not merely discouraged.
	p = types.DefaultParams()
	p.RoleRewardInflationShare = math.LegacyNewDecWithPrec(101, 2)
	require.Error(t, p.Validate())

	op := types.DefaultRepOperationalParams()
	require.Equal(t, types.DefaultParams().RoleRewardInflationShare, op.RoleRewardInflationShare)
	op.RoleRewardInflationShare = math.LegacyNewDec(2)
	require.Error(t, op.Validate())
}

func TestQueryRoleRewardPools(t *testing.T) {
	f := initFixture(t)
	ledger, _, mk := installFunding(t, f, math.NewInt(1_000_000_000_000))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxSentinelRewardPool = math.NewInt(80_000)
	params.MaxReviewerRewardPool = math.NewInt(20_000)
	params.MaxCuratorRewardPool = math.ZeroInt()  // keep this case a 2-pool split
	params.MaxVerifierRewardPool = math.ZeroInt() // ditto
	setDailyAllowance(mk, 50_000)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))

	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.RoleRewardPools(f.ctx, &types.QueryRoleRewardPoolsRequest{})
	require.NoError(t, err)
	// The query reports every pool in fundedRolePools, including the curator
	// and verifier pools zeroed above -- a role with funding switched off
	// should still be visible, or "why is nobody being paid" has no answer.
	require.Len(t, resp.Pools, 4)
	require.Equal(t, math.NewInt(50_000), resp.FundedToday)
	require.Equal(t, math.NewInt(50_000), resp.DailyFundingCap)

	byRole := map[string]types.RoleRewardPoolStatus{}
	for _, p := range resp.Pools {
		byRole[p.Role] = p
	}
	sentinel := byRole["content_sentinel"]
	require.Equal(t, keeper.SentinelRewardPoolAddress().String(), sentinel.Address)
	require.Equal(t, balanceOf(ledger, keeper.SentinelRewardPoolAddress()), sentinel.Balance)
	require.Equal(t, math.NewInt(80_000), sentinel.Cap)
	require.Equal(t, sentinel.Cap.Sub(sentinel.Balance), sentinel.Headroom)

	reviewer := byRole["initiative_reviewer"]
	require.Equal(t, keeper.ReviewerRewardPoolAddress().String(), reviewer.Address)
	require.Equal(t, math.NewInt(20_000), reviewer.Cap)

	// Headroom is clamped at zero: an over-funded pool must not report a
	// negative share and pull the division below zero.
	ledger[keeper.ReviewerRewardPoolAddress().String()] = math.NewInt(99_999)
	resp, err = qs.RoleRewardPools(f.ctx, &types.QueryRoleRewardPoolsRequest{})
	require.NoError(t, err)
	for _, p := range resp.Pools {
		require.False(t, p.Headroom.IsNegative())
	}

	_, err = qs.RoleRewardPools(f.ctx, nil)
	require.Error(t, err)
}

func TestFundRoleRewardPoolsSweepsStrandedIntake(t *testing.T) {
	f := initFixture(t)
	ledger, _, mk := installFunding(t, f, math.NewInt(1_000_000_000_000))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxSentinelRewardPool = math.NewInt(1_000_000)
	params.MaxReviewerRewardPool = math.NewInt(1_000_000)
	params.MaxCuratorRewardPool = math.ZeroInt()
	params.MaxVerifierRewardPool = math.ZeroInt()
	setDailyAllowance(mk, 10_000)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Simulate a placement that failed on an earlier block: SPARK sitting in
	// the intake with nothing scheduled to move it. Without the sweep it would
	// be stranded there permanently.
	ledger[keeper.RoleRewardIntakeAddress().String()] = math.NewInt(4_000)

	require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))
	total := balanceOf(ledger, keeper.SentinelRewardPoolAddress()).
		Add(balanceOf(ledger, keeper.ReviewerRewardPoolAddress()))
	require.Equal(t, math.NewInt(14_000), total, "the stranded 4,000 must be placed alongside the new draw")
	require.True(t, balanceOf(ledger, keeper.RoleRewardIntakeAddress()).IsZero())
}

func TestRepSubAddressesAreDistinct(t *testing.T) {
	// Every one of these is an ordinary bank account, and the partition between
	// pools is enforced by nothing but the addresses differing. A duplicated
	// derivation key would silently merge two pools — the reviewer pool paying
	// out of sentinel money, or the intake spending a live pool's balance.
	addrs := map[string]string{
		"sentinel_rewards":   keeper.SentinelRewardPoolAddress().String(),
		"reviewer_rewards":   keeper.ReviewerRewardPoolAddress().String(),
		"verifier_rewards":   keeper.VerifierRewardPoolAddress().String(),
		"role_reward_intake": keeper.RoleRewardIntakeAddress().String(),
		"tag_budgets":        keeper.TagBudgetEscrowAddress().String(),
		"appeal_bonds":       keeper.AppealBondEscrowAddress().String(),
	}
	seen := map[string]string{}
	for name, addr := range addrs {
		if prev, dup := seen[addr]; dup {
			t.Fatalf("%s and %s derive the same address %s", prev, name, addr)
		}
		seen[addr] = name
	}
	require.Len(t, seen, len(addrs))
}

// Both of these collections are consensus-relevant bookkeeping with no other
// home: dropping either on an export/import round-trip silently changes
// behaviour rather than erroring, which is exactly the kind of gap a genesis
// round-trip test exists to catch.
func TestGenesisRoundTripsFundingAndEscalationState(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.RoleRewardDayFunding.Set(f.ctx, 20301, math.NewInt(777_000).String()))
	require.NoError(t, f.keeper.RoleRewardDayFunding.Set(f.ctx, 20302, math.NewInt(1_000).String()))
	require.NoError(t, f.keeper.EscalatedReviews.Set(f.ctx, 41))
	require.NoError(t, f.keeper.EscalatedReviews.Set(f.ctx, 42))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.RoleRewardDayFundingList, 2, "the day ledger must survive export")
	require.Len(t, exported.EscalatedReviewList, 2, "escalation markers must survive export")

	// Import into a clean chain and confirm the state is byte-for-byte back.
	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))

	require.Equal(t, math.NewInt(777_000), g.keeper.GetRoleRewardDayFunding(g.ctx, 20301),
		"a restarted chain must not get a fresh daily allowance")
	require.Equal(t, math.NewInt(1_000), g.keeper.GetRoleRewardDayFunding(g.ctx, 20302))
	for _, id := range []uint64{41, 42} {
		has, hErr := g.keeper.EscalatedReviews.Has(g.ctx, id)
		require.NoError(t, hErr)
		require.True(t, has, "initiative %d would be re-escalated and its deadline extended again", id)
	}
}

func TestRoleRewardCapsAreBoundedAgainstOverflow(t *testing.T) {
	// These caps are committee-editable and feed a multiplication in
	// FundRoleRewardPools. math.Int panics past 256 bits, and a panic in
	// BeginBlock halts the chain — so an unbounded cap makes a mistyped params
	// proposal a liveness bug rather than a bad setting.
	huge, ok := math.NewIntFromString("57896044618658097711785492504343953926634992332820282019728792003956564819968") // 2^255
	require.True(t, ok)

	for _, tc := range []struct {
		name  string
		apply func(p *types.Params)
	}{
		{"sentinel pool", func(p *types.Params) { p.MaxSentinelRewardPool = huge }},
		{"reviewer pool", func(p *types.Params) { p.MaxReviewerRewardPool = huge }},
		{"curator pool", func(p *types.Params) { p.MaxCuratorRewardPool = huge }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.apply(&p)
			require.Error(t, p.Validate(), "validation must reject a cap past the ceiling")

			op := types.DefaultRepOperationalParams()
			switch tc.name {
			case "sentinel pool":
				op.MaxSentinelRewardPool = huge
			case "reviewer pool":
				op.MaxReviewerRewardPool = huge
			default:
				op.MaxCuratorRewardPool = huge
			}
			require.Error(t, op.Validate(), "the committee-editable mirror must reject it too")
		})
	}

	// Defence in depth: even with the ceiling written straight into state,
	// bypassing validation, BeginBlock must not panic.
	f := initFixture(t)
	_, _, mk := installFunding(t, f, math.NewInt(1_000_000_000_000))
	setDailyAllowance(mk, 500_000)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxSentinelRewardPool = huge
	params.MaxReviewerRewardPool = huge
	params.MaxCuratorRewardPool = huge
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NotPanics(t, func() {
		require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	}, "an out-of-range cap must not be able to halt the chain")
}

func TestUtcDayBucketIsStableBeforeTheEpoch(t *testing.T) {
	// A zero block time makes Unix() negative; converting that to uint64 wraps
	// to an enormous bucket, which would make the day ledger meaningless at
	// genesis and effectively restart the cap on the first real block.
	f := initFixture(t)
	_, dk, mk := installFunding(t, f, math.NewInt(1_000_000_000))
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	setDailyAllowance(mk, 50_000)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	zeroTime := f.ctx.WithBlockTime(time.Time{})
	require.NoError(t, f.keeper.FundRoleRewardPools(zeroTime))
	require.Equal(t, math.NewInt(50_000), f.keeper.GetRoleRewardDayFunding(zeroTime, 0),
		"a pre-epoch block time buckets to day 0, not a wrapped one")

	// And the same block draws no more than one allowance.
	before := dk.pool
	require.NoError(t, f.keeper.FundRoleRewardPools(zeroTime))
	require.Equal(t, before, dk.pool)
}

func TestGenesisValidateRejectsMalformedFundingState(t *testing.T) {
	base := func() types.GenesisState {
		g := types.DefaultGenesis()
		return *g
	}

	dup := base()
	dup.RoleRewardDayFundingList = []types.RoleRewardDayFunding{
		{Day: 7, AmountFunded: math.NewInt(10)},
		{Day: 7, AmountFunded: math.NewInt(20)},
	}
	require.Error(t, dup.Validate(), "a duplicate day would collapse on import and refund part of a spent allowance")

	neg := base()
	neg.RoleRewardDayFundingList = []types.RoleRewardDayFunding{{Day: 7, AmountFunded: math.NewInt(-1)}}
	require.Error(t, neg.Validate())

	nilAmt := base()
	nilAmt.RoleRewardDayFundingList = []types.RoleRewardDayFunding{{Day: 7}}
	require.Error(t, nilAmt.Validate())

	dupEsc := base()
	dupEsc.EscalatedReviewList = []uint64{9, 9}
	require.Error(t, dupEsc.Validate())

	ok := base()
	ok.RoleRewardDayFundingList = []types.RoleRewardDayFunding{{Day: 7, AmountFunded: math.NewInt(10)}}
	ok.EscalatedReviewList = []uint64{9, 10}
	require.NoError(t, ok.Validate())
}

// The daily allowance is a share of the community pool's INFLATION INCOME, not
// a fixed amount and not a share of the pool balance. These tests pin all three
// properties, because each was a real alternative that would have been wrong.
func TestDailyAllowanceIsAShareOfInflationIncome(t *testing.T) {
	f := initFixture(t)
	_, dk, mk := installFunding(t, f, math.NewInt(1_000_000_000_000))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.RoleRewardInflationShare = math.LegacyNewDecWithPrec(5, 1) // 0.5
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	dk.tax = math.LegacyNewDecWithPrec(15, 2) // 15% community tax

	// 100M SPARK supply at 5% inflation -> 5M SPARK/yr of provisions.
	// 5,000,000 * 0.15 * 0.5 / 365 = 1,027 SPARK/day.
	mk.provisions = math.LegacyNewDec(5_000_000_000_000) // uspark
	require.Equal(t, math.NewInt(1_027_397_260),
		f.keeper.RoleRewardDailyAllowanceForTest(f.ctx, params))

	// Same share at the 2% floor yields proportionally less -- the point of the
	// design. A fixed amount would instead take its LARGEST share of the pool
	// exactly here, where the pool is poorest.
	mk.provisions = math.LegacyNewDec(2_000_000_000_000)
	require.Equal(t, math.NewInt(410_958_904),
		f.keeper.RoleRewardDailyAllowanceForTest(f.ctx, params))
}

func TestDailyAllowanceIgnoresTheCommunityPoolBalance(t *testing.T) {
	// The genesis community pool holds 95M SPARK earmarked for the councils via
	// x/split. Sizing the draw off the balance would let x/rep raid it, and
	// would also take a cut of every direct fund-community-pool deposit.
	f := initFixture(t)
	_, dk, mk := installFunding(t, f, math.NewInt(95_000_000_000_000)) // 95M SPARK

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.RoleRewardInflationShare = math.LegacyNewDecWithPrec(5, 1)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	dk.tax = math.LegacyNewDecWithPrec(15, 2)
	mk.provisions = math.LegacyNewDec(2_000_000_000_000)

	before := dk.pool
	require.NoError(t, f.keeper.FundRoleRewardPools(f.ctx))
	drawn := before.Sub(dk.pool)
	require.Equal(t, math.NewInt(410_958_904), drawn,
		"an enormous pool balance must not enlarge the draw")
}

func TestDailyAllowanceZeroWhenNothingIsBeingMinted(t *testing.T) {
	f := initFixture(t)
	_, dk, mk := installFunding(t, f, math.NewInt(1_000_000_000_000))
	mk.provisions = math.LegacyZeroDec()

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	require.True(t, f.keeper.RoleRewardDailyAllowanceForTest(f.ctx, params).IsZero())
	require.Equal(t, math.NewInt(1_000_000_000_000), dk.pool, "no provisions, no draw")
}

func TestDailyAllowanceZeroWithoutMintKeeper(t *testing.T) {
	// Late-wired, so an unwired chain must simply not fund rather than panic.
	f := initFixture(t)
	ledger := installLedger(f)
	dk := &mockDistrKeeper{pool: math.NewInt(1_000), denom: f.keeper.BondDenom(f.ctx),
		ledger: ledger, tax: math.LegacyOneDec()}
	f.keeper.SetDistrKeeper(dk)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	require.Equal(t, math.NewInt(1_000), dk.pool)
}
