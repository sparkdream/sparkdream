package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// sentinelRewardFixture bundles the rep fixture with a wired forum mock and a
// params override that makes the sentinel-reward cadence easy to trigger
// (epoch_blocks = 10, min_appeals = 10, accuracy_floor = 0.70).
type sentinelRewardFixture struct {
	*fixture
	fk *mockForumKeeper
}

func newSentinelRewardFixture(t *testing.T) *sentinelRewardFixture {
	t.Helper()

	params := types.DefaultParams()
	params.SentinelRewardEpochBlocks = 10
	params.MinAppealsForAccuracy = 10
	params.MinEpochActivityForReward = 1
	params.MinAppealRate = math.LegacyNewDecWithPrec(5, 2) // 0.05
	params.MinSentinelAccuracy = math.LegacyNewDecWithPrec(70, 2)

	f := initFixture(t, WithCustomParams(params))

	fk := &mockForumKeeper{
		authors:         make(map[uint64]string),
		tags:            make(map[uint64][]string),
		actionSentinels: make(map[string]string),
		counters:        make(map[string]types.SentinelActivityCounters),
	}
	f.keeper.SetForumKeeper(fk)

	return &sentinelRewardFixture{fixture: f, fk: fk}
}

// seedSentinel inserts a BondedRole record (role = FORUM_SENTINEL) with the
// given bond status and optional counters. Returns the bech32 address string.
func (rf *sentinelRewardFixture) seedSentinel(
	t *testing.T,
	seed []byte,
	status types.BondedRoleStatus,
	counters types.SentinelActivityCounters,
) string {
	t.Helper()
	if len(seed) != 20 {
		// pad/truncate to 20 bytes
		buf := make([]byte, 20)
		copy(buf, seed)
		seed = buf
	}
	addr := sdk.AccAddress(seed)
	addrStr, err := rf.addressCodec.BytesToString(addr)
	require.NoError(t, err)

	key := collections.Join(int32(types.RoleType_ROLE_TYPE_FORUM_SENTINEL), addrStr)
	require.NoError(t, rf.keeper.BondedRoles.Set(rf.ctx, key, types.BondedRole{
		Address:    addrStr,
		RoleType:   types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		BondStatus: status,
	}))
	rf.fk.counters[addrStr] = counters
	return addrStr
}

// happyCounters returns a counters record that easily passes every gate:
// 20 total decided appeals, 16 upheld -> accuracy 0.80; epoch_appeals_filed=2
// vs epoch_hides=10 -> appeal_rate 0.20 (>= 0.05); epoch_activity=15.
func happyCounters() types.SentinelActivityCounters {
	return types.SentinelActivityCounters{
		UpheldHides:          10,
		OverturnedHides:      4,
		UpheldLocks:          4,
		OverturnedLocks:      1,
		UpheldMoves:          2,
		OverturnedMoves:      0,
		EpochHides:           10,
		EpochLocks:           3,
		EpochMoves:           2,
		EpochPins:            0,
		EpochAppealsFiled:    2,
		EpochAppealsResolved: 4,
	}
}

func TestIsSentinelRewardEpoch(t *testing.T) {
	rf := newSentinelRewardFixture(t)

	// Block 0 -> never an epoch.
	rf.ctx = rf.ctx.WithBlockHeight(0)
	require.False(t, rf.keeper.IsSentinelRewardEpoch(rf.ctx))

	// Block 5 with cadence 10 -> not an epoch.
	rf.ctx = rf.ctx.WithBlockHeight(5)
	require.False(t, rf.keeper.IsSentinelRewardEpoch(rf.ctx))

	// Block 10 -> epoch boundary.
	rf.ctx = rf.ctx.WithBlockHeight(10)
	require.True(t, rf.keeper.IsSentinelRewardEpoch(rf.ctx))

	// Block 30 -> epoch boundary.
	rf.ctx = rf.ctx.WithBlockHeight(30)
	require.True(t, rf.keeper.IsSentinelRewardEpoch(rf.ctx))
}

func TestDistributeSentinelRewards_NonEpochNoOp(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(3) // not a boundary

	addr := rf.seedSentinel(t, []byte("happy-sentinel-aaaa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, happyCounters())

	// Pool has 1_000 SPARK.
	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, math.NewInt(1_000))
	}
	var sendCount int
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, _ sdk.Coins) error {
		sendCount++
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))
	require.Zero(t, sendCount, "no distribution outside of an epoch boundary")
	require.Empty(t, rf.fk.resetAddrs, "no counter resets outside of an epoch boundary")

	// Counters untouched.
	_ = addr
	stillThere := rf.fk.counters[addr]
	require.Equal(t, uint64(10), stillThere.EpochHides)
}

func TestDistributeSentinelRewards_PoolEmpty(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	addr := rf.seedSentinel(t, []byte("happy-sentinel-aaaa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, happyCounters())

	// Pool empty.
	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, math.ZeroInt())
	}
	sendCalled := false
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, _ sdk.Coins) error {
		sendCalled = true
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))
	require.False(t, sendCalled, "no distribution when pool is empty")

	// Counters still reset.
	require.Contains(t, rf.fk.resetAddrs, addr)
	require.Equal(t, uint64(0), rf.fk.counters[addr].EpochHides, "epoch counters reset")
	require.Equal(t, uint64(0), rf.fk.counters[addr].EpochLocks)
	require.Equal(t, uint64(0), rf.fk.counters[addr].EpochMoves)
}

func TestDistributeSentinelRewards_NoEligibleSentinels(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	// Sentinel with too-few decided appeals (gate 2 fails).
	bad := types.SentinelActivityCounters{
		UpheldHides:          2,
		OverturnedHides:      1, // only 3 decided, below MinAppealsForAccuracy=10
		EpochHides:           5,
		EpochAppealsFiled:    1,
		EpochAppealsResolved: 1,
	}
	addr := rf.seedSentinel(t, []byte("bad-sentinel-aaaaa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, bad)

	// Pool has funds.
	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, math.NewInt(1_000))
	}
	sendCount := 0
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, _ sdk.Coins) error {
		sendCount++
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))
	require.Zero(t, sendCount, "nobody eligible -> no distribution")
	require.Contains(t, rf.fk.resetAddrs, addr, "counters still reset")
}

func TestDistributeSentinelRewards_HappyPath(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	// Two eligible sentinels with different scores.
	c1 := happyCounters() // strong activity
	c2 := happyCounters()
	// Give c2 half the epoch_appeals_resolved to produce a different score.
	c2.EpochAppealsResolved = 1
	c2.EpochHides = 5
	c2.EpochLocks = 1
	c2.EpochMoves = 1

	a1 := rf.seedSentinel(t, []byte("alpha-sentinel-aaaaa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, c1)
	a2 := rf.seedSentinel(t, []byte("beta-sentinel-bbbbbb"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, c2)

	poolAmount := math.NewInt(10_000)
	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, poolAmount)
	}
	sent := map[string]math.Int{}
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, fromAddr sdk.AccAddress, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		require.True(t, fromAddr.Equals(keeper.SentinelRewardPoolAddress()))
		s, _ := rf.addressCodec.BytesToString(recipientAddr)
		sent[s] = amt.AmountOf(types.RewardDenom)
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))

	// Both sentinels received something.
	alloc1, ok := sent[a1]
	require.True(t, ok, "alpha received payout")
	require.True(t, alloc1.IsPositive())
	alloc2, ok := sent[a2]
	require.True(t, ok, "beta received payout")
	require.True(t, alloc2.IsPositive())

	// Pro-rata: alpha should receive more than beta because its score is higher
	// (more epoch_appeals_resolved + higher activity bonuses).
	require.True(t, alloc1.GT(alloc2), "expected alpha > beta: alpha=%s beta=%s", alloc1, alloc2)

	// Allocations sum to <= pool (truncation leaves dust).
	require.True(t, alloc1.Add(alloc2).LTE(poolAmount))

	// CumulativeRewards incremented.
	sa1, err := rf.keeper.BondedRoles.Get(rf.ctx, collections.Join(int32(types.RoleType_ROLE_TYPE_FORUM_SENTINEL), a1))
	require.NoError(t, err)
	require.Equal(t, alloc1.String(), sa1.CumulativeRewards)
	require.Equal(t, int64(1), sa1.LastRewardEpoch, "epoch_num = 10 / 10 = 1")

	sa2, err := rf.keeper.BondedRoles.Get(rf.ctx, collections.Join(int32(types.RoleType_ROLE_TYPE_FORUM_SENTINEL), a2))
	require.NoError(t, err)
	require.Equal(t, alloc2.String(), sa2.CumulativeRewards)

	// Counters reset on both.
	require.Contains(t, rf.fk.resetAddrs, a1)
	require.Contains(t, rf.fk.resetAddrs, a2)
}

func TestDistributeSentinelRewards_DemotedExcluded(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	// One DEMOTED sentinel with otherwise-happy counters; one NORMAL.
	demoted := rf.seedSentinel(t, []byte("demoted-sentinel-aa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, happyCounters())
	normal := rf.seedSentinel(t, []byte("normal-sentinel-aaa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, happyCounters())

	poolAmount := math.NewInt(10_000)
	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, poolAmount)
	}
	sent := map[string]math.Int{}
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		s, _ := rf.addressCodec.BytesToString(recipientAddr)
		sent[s] = amt.AmountOf(types.RewardDenom)
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))

	// Only the non-demoted one got paid.
	_, gotDemoted := sent[demoted]
	require.False(t, gotDemoted, "demoted sentinel excluded")

	allocNormal, ok := sent[normal]
	require.True(t, ok)
	// Full pool routed to the single eligible sentinel (modulo truncation).
	require.True(t, allocNormal.GT(math.ZeroInt()))

	// Counters reset on both regardless.
	require.Contains(t, rf.fk.resetAddrs, demoted)
	require.Contains(t, rf.fk.resetAddrs, normal)
}

func TestDistributeSentinelRewards_AppealRateGate(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	// epoch_hides=100, epoch_appeals_filed=1 -> appeal_rate=0.01 < 0.05.
	c := happyCounters()
	c.EpochHides = 100
	c.EpochAppealsFiled = 1
	rf.seedSentinel(t, []byte("low-appeal-sentinel"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, c)

	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, math.NewInt(1_000))
	}
	sendCount := 0
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, _ sdk.Coins) error {
		sendCount++
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))
	require.Zero(t, sendCount, "low appeal rate excludes sentinel")
}

func TestDistributeSentinelRewards_AccuracyGate(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	// accuracy = 5/20 = 0.25, below 0.70.
	c := types.SentinelActivityCounters{
		UpheldHides:          5,
		OverturnedHides:      15,
		EpochHides:           10,
		EpochAppealsFiled:    2,
		EpochAppealsResolved: 3,
	}
	rf.seedSentinel(t, []byte("low-acc-sentinel--"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, c)

	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, math.NewInt(1_000))
	}
	sendCount := 0
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, _ sdk.Coins) error {
		sendCount++
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))
	require.Zero(t, sendCount, "sub-threshold accuracy excludes sentinel")
}

func TestDistributeSentinelRewards_SinglePayoutFullPool(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	only := rf.seedSentinel(t, []byte("solo-sentinel-aaaaa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, happyCounters())

	pool := math.NewInt(500_000)
	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, pool)
	}
	var got math.Int
	got = math.ZeroInt()
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, amt sdk.Coins) error {
		got = amt.AmountOf(types.RewardDenom)
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))
	require.Equal(t, pool, got, "only eligible sentinel gets the whole pool (score/total_score = 1)")

	sa, err := rf.keeper.BondedRoles.Get(rf.ctx, collections.Join(int32(types.RoleType_ROLE_TYPE_FORUM_SENTINEL), only))
	require.NoError(t, err)
	require.Equal(t, pool.String(), sa.CumulativeRewards)
}
