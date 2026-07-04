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

// sentinelRewardFixture bundles the rep fixture with a params override that
// makes the sentinel-reward cadence easy to trigger (epoch_blocks = 10,
// min_appeals = 10, accuracy_floor = 0.70). Since the RoleActivity migration
// the distribution is fully rep-internal: tests seed RoleActivity records in
// the rep store directly — no forum mock involved.
type sentinelRewardFixture struct {
	*fixture
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
	return &sentinelRewardFixture{fixture: f}
}

// sentinelCounters mirrors the pre-migration counter shape so the test
// bodies keep their vocabulary; roleActivityFromCounters converts it to a
// RoleActivity record.
type sentinelCounters struct {
	UpheldHides, OverturnedHides   uint64
	UpheldLocks, OverturnedLocks   uint64
	UpheldMoves, OverturnedMoves   uint64
	EpochHides, EpochLocks         uint64
	EpochMoves, EpochPins          uint64
	EpochCurations                 uint64
	EpochAppealsFiled              uint64
	EpochAppealsResolved           uint64
	WindowUpheld, WindowOverturned uint64 // rolling-window tallies at the current epoch
}

// seedSentinel inserts a BondedRole record (role = CONTENT_SENTINEL) with
// the given bond status plus a RoleActivity record derived from `c`. The
// accuracy ring gets a single bucket stamped at reward epoch 1 (the tests
// run at block 10 with epoch_blocks 10) carrying the window tallies.
// Returns the bech32 address string.
func (rf *sentinelRewardFixture) seedSentinel(
	t *testing.T,
	seed []byte,
	status types.BondedRoleStatus,
	c sentinelCounters,
) string {
	t.Helper()
	if len(seed) != 20 {
		buf := make([]byte, 20)
		copy(buf, seed)
		seed = buf
	}
	addr := sdk.AccAddress(seed)
	addrStr, err := rf.addressCodec.BytesToString(addr)
	require.NoError(t, err)

	key := collections.Join(int32(types.RoleType_ROLE_TYPE_CONTENT_SENTINEL), addrStr)
	require.NoError(t, rf.keeper.BondedRoles.Set(rf.ctx, key, types.BondedRole{
		Address:    addrStr,
		RoleType:   types.RoleType_ROLE_TYPE_CONTENT_SENTINEL,
		BondStatus: status,
	}))

	ra := types.RoleActivity{
		RoleType: types.RoleType_ROLE_TYPE_CONTENT_SENTINEL,
		Address:  addrStr,
		EpochActions: map[string]uint64{
			types.ActionKindForumHide:        c.EpochHides,
			types.ActionKindForumLock:        c.EpochLocks,
			types.ActionKindForumMove:        c.EpochMoves,
			types.ActionKindForumPin:         c.EpochPins,
			types.ActionKindForumCuration:    c.EpochCurations,
			types.ActionKindForumAppealFiled: c.EpochAppealsFiled,
		},
		TotalActions: map[string]uint64{
			types.ActionKindForumHide: c.UpheldHides + c.OverturnedHides,
			types.ActionKindForumLock: c.UpheldLocks + c.OverturnedLocks,
			types.ActionKindForumMove: c.UpheldMoves + c.OverturnedMoves,
		},
		UpheldActions: map[string]uint64{
			types.ActionKindForumHide: c.UpheldHides,
			types.ActionKindForumLock: c.UpheldLocks,
			types.ActionKindForumMove: c.UpheldMoves,
		},
		OverturnedActions: map[string]uint64{
			types.ActionKindForumHide: c.OverturnedHides,
			types.ActionKindForumLock: c.OverturnedLocks,
			types.ActionKindForumMove: c.OverturnedMoves,
		},
		EpochAppealsResolved: c.EpochAppealsResolved,
		AccuracyWindow: []*types.RoleAccuracyBucket{
			{Epoch: 1, Upheld: c.WindowUpheld, Overturned: c.WindowOverturned},
		},
	}
	require.NoError(t, rf.keeper.RoleActivities.Set(rf.ctx, key, ra))
	return addrStr
}

// epochActions reads the current epoch-action map for assertions on resets.
func (rf *sentinelRewardFixture) epochActions(t *testing.T, addr string) map[string]uint64 {
	t.Helper()
	ra, err := rf.keeper.GetRoleActivity(rf.ctx, types.RoleType_ROLE_TYPE_CONTENT_SENTINEL, addr)
	require.NoError(t, err)
	return ra.EpochActions
}

// happyCounters returns a counters record that easily passes every gate:
// 20 in-window decided appeals, 16 upheld -> accuracy 0.80;
// epoch_appeals_filed=2 vs epoch_hides=10 -> appeal_rate 0.20 (>= 0.05);
// epoch_activity=15.
func happyCounters() sentinelCounters {
	return sentinelCounters{
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
		WindowUpheld:         16,
		WindowOverturned:     5,
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

	// Counters untouched.
	require.Equal(t, uint64(10), rf.epochActions(t, addr)[types.ActionKindForumHide],
		"no counter resets outside of an epoch boundary")
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
	actions := rf.epochActions(t, addr)
	require.Equal(t, uint64(0), actions[types.ActionKindForumHide], "epoch counters reset")
	require.Equal(t, uint64(0), actions[types.ActionKindForumLock])
	require.Equal(t, uint64(0), actions[types.ActionKindForumMove])
}

func TestDistributeSentinelRewards_NoEligibleSentinels(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	// Sentinel with too-few decided appeals (gate 2 fails).
	bad := sentinelCounters{
		UpheldHides:          2,
		OverturnedHides:      1, // only 3 decided, below MinAppealsForAccuracy=10
		EpochHides:           5,
		EpochAppealsFiled:    1,
		EpochAppealsResolved: 1,
		WindowUpheld:         2,
		WindowOverturned:     1,
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
	require.Equal(t, uint64(0), rf.epochActions(t, addr)[types.ActionKindForumHide], "counters still reset")
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
		sent[s] = amt.AmountOf("uspark")
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
	sa1, err := rf.keeper.BondedRoles.Get(rf.ctx, collections.Join(int32(types.RoleType_ROLE_TYPE_CONTENT_SENTINEL), a1))
	require.NoError(t, err)
	require.Equal(t, alloc1.String(), sa1.CumulativeRewards)
	require.Equal(t, int64(1), sa1.LastRewardEpoch, "epoch_num = 10 / 10 = 1")

	sa2, err := rf.keeper.BondedRoles.Get(rf.ctx, collections.Join(int32(types.RoleType_ROLE_TYPE_CONTENT_SENTINEL), a2))
	require.NoError(t, err)
	require.Equal(t, alloc2.String(), sa2.CumulativeRewards)

	// Counters reset on both.
	require.Empty(t, rf.epochActions(t, a1))
	require.Empty(t, rf.epochActions(t, a2))
}

func TestDistributeSentinelRewards_CurationOnlyEligible(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	// A sentinel whose ONLY epoch activity is curations (no hides/locks/moves
	// this epoch) but with enough decided appeals + accuracy to clear the gates.
	// Proves epoch curations both satisfy the epoch-activity gate and feed the
	// score via the curation weight.
	c := happyCounters()
	c.EpochHides = 0
	c.EpochLocks = 0
	c.EpochMoves = 0
	c.EpochPins = 0
	c.EpochAppealsFiled = 0
	c.EpochAppealsResolved = 0
	c.EpochCurations = 5

	addr := rf.seedSentinel(t, []byte("curator-sentinel-aa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, c)

	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, math.NewInt(10_000))
	}
	sent := map[string]math.Int{}
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		s, _ := rf.addressCodec.BytesToString(recipientAddr)
		sent[s] = amt.AmountOf("uspark")
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))

	alloc, ok := sent[addr]
	require.True(t, ok, "curation-only sentinel received a payout")
	require.True(t, alloc.IsPositive())
	require.Empty(t, rf.epochActions(t, addr), "epoch counters reset")
}

// TestDistributeSentinelRewards_CollectOnlyEligible proves a moderator whose
// ONLY epoch activity is collect hides passes the activity gate and earns a
// score bonus — the cross-surface reward property the RoleActivity migration
// exists to guarantee.
func TestDistributeSentinelRewards_CollectOnlyEligible(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	c := happyCounters()
	c.EpochHides = 0
	c.EpochLocks = 0
	c.EpochMoves = 0
	c.EpochPins = 0
	c.EpochAppealsFiled = 0

	addr := rf.seedSentinel(t, []byte("collect-sentinel-aa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, c)

	// Add collect-side activity directly on the record.
	key := collections.Join(int32(types.RoleType_ROLE_TYPE_CONTENT_SENTINEL), addr)
	ra, err := rf.keeper.RoleActivities.Get(rf.ctx, key)
	require.NoError(t, err)
	ra.EpochActions[types.ActionKindCollectHide] = 6
	ra.EpochActions[types.ActionKindCollectAppealFiled] = 1
	require.NoError(t, rf.keeper.RoleActivities.Set(rf.ctx, key, ra))

	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, math.NewInt(10_000))
	}
	sent := map[string]math.Int{}
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		s, _ := rf.addressCodec.BytesToString(recipientAddr)
		sent[s] = amt.AmountOf("uspark")
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))

	alloc, ok := sent[addr]
	require.True(t, ok, "collect-only moderator received a payout")
	require.True(t, alloc.IsPositive())
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
		sent[s] = amt.AmountOf("uspark")
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
	require.Empty(t, rf.epochActions(t, demoted))
	require.Empty(t, rf.epochActions(t, normal))
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
	c := sentinelCounters{
		UpheldHides:          5,
		OverturnedHides:      15,
		EpochHides:           10,
		EpochAppealsFiled:    2,
		EpochAppealsResolved: 3,
		WindowUpheld:         5,
		WindowOverturned:     15,
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
		got = amt.AmountOf("uspark")
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))
	require.Equal(t, pool, got, "only eligible sentinel gets the whole pool (score/total_score = 1)")

	sa, err := rf.keeper.BondedRoles.Get(rf.ctx, collections.Join(int32(types.RoleType_ROLE_TYPE_CONTENT_SENTINEL), only))
	require.NoError(t, err)
	require.Equal(t, pool.String(), sa.CumulativeRewards)
}

// TestDistributeSentinelRewards_EmptyWindowIneligible proves the accuracy gates
// read the rolling window, not lifetime counters: a sentinel with a strong
// lifetime record but no in-window resolved appeals (the cutover / inactivity
// case) is ineligible.
func TestDistributeSentinelRewards_EmptyWindowIneligible(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	// Strong lifetime record, but zero the window tallies.
	c := happyCounters()
	c.WindowUpheld = 0
	c.WindowOverturned = 0
	addr := rf.seedSentinel(t, []byte("stale-sentinel-aaaa"),
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
	require.Zero(t, sendCount, "empty window -> below MinAppealsForAccuracy -> ineligible")
	require.Empty(t, rf.epochActions(t, addr), "epoch counters still reset")
}

// TestDistributeSentinelRewards_WindowDrivesAccuracy proves the inverse: an
// empty lifetime record but a populated, high-accuracy window IS eligible.
func TestDistributeSentinelRewards_WindowDrivesAccuracy(t *testing.T) {
	rf := newSentinelRewardFixture(t)
	rf.ctx = rf.ctx.WithBlockHeight(10)

	// Zero lifetime upheld/overturned; window carries 12 upheld / 16 decided
	// = 0.75 (>= 0.70), 16 decided (>= MinAppealsForAccuracy=10).
	c := sentinelCounters{
		EpochHides:           10,
		EpochAppealsFiled:    2,
		EpochAppealsResolved: 4,
		WindowUpheld:         12,
		WindowOverturned:     4,
	}
	addr := rf.seedSentinel(t, []byte("fresh-sentinel-aaaa"),
		types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, c)

	pool := math.NewInt(500_000)
	rf.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, pool)
	}
	got := math.ZeroInt()
	rf.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, amt sdk.Coins) error {
		got = amt.AmountOf("uspark")
		return nil
	}

	require.NoError(t, rf.keeper.DistributeSentinelRewards(rf.ctx))
	require.Equal(t, pool, got, "high-accuracy window alone makes the sentinel eligible")
	_ = addr
}
