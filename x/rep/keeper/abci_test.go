package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// EndBlocker is a large orchestrator that delegates to many sub-systems covered
// by their own tests. This file verifies only the high-level wiring and the
// invariants unique to abci.go: (1) EndBlocker runs cleanly on an empty store,
// (2) the epoch-boundary bulk decay fires at the top of EndBlocker and then
// stays idempotent within the same epoch, and (3) BurnSentinelRewardPoolOverflow
// and ExpireTags are callable in isolation.

func TestEndBlocker_NoopOnEmptyFixture(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
}

func TestEndBlocker_BulkDecayRunsOncePerEpoch(t *testing.T) {
	params := types.DefaultParams()
	params.EpochBlocks = 10
	params.UnstakedDecayRate = math.LegacyNewDecWithPrec(2, 3) // 0.2%
	params.NewMemberDecayGraceEpochs = 0                       // no grace, so decay bites immediately
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	addr := sdk.AccAddress([]byte("alice")).String()
	initial := math.NewInt(1_000_000)
	zero := math.ZeroInt()
	require.NoError(t, k.Member.Set(f.ctx, addr, types.Member{
		Address:        addr,
		DreamBalance:   &initial,
		StakedDream:    &zero,
		LifetimeEarned: &zero,
		LifetimeBurned: &zero,
		LastDecayEpoch: 0,
	}))

	// Advance to epoch 5, run EndBlocker.
	ctx1 := sdkCtx.WithBlockHeight(55)
	require.NoError(t, k.EndBlocker(ctx1))

	m, err := k.Member.Get(ctx1, addr)
	require.NoError(t, err)
	require.Equal(t, int64(5), m.LastDecayEpoch, "bulk decay should have advanced LastDecayEpoch to current epoch")
	afterFirst := *m.DreamBalance
	require.True(t, afterFirst.LT(initial), "balance should have decayed")

	// Running EndBlocker again in the same epoch must be a no-op on balances.
	require.NoError(t, k.EndBlocker(ctx1))
	m, err = k.Member.Get(ctx1, addr)
	require.NoError(t, err)
	require.Equal(t, afterFirst, *m.DreamBalance, "no further decay within the same epoch")

	// New epoch, fresh decay pass runs.
	ctx2 := sdkCtx.WithBlockHeight(65)
	require.NoError(t, k.EndBlocker(ctx2))
	m, err = k.Member.Get(ctx2, addr)
	require.NoError(t, err)
	require.Equal(t, int64(6), m.LastDecayEpoch)
	require.True(t, m.DreamBalance.LT(afterFirst), "decay should apply in the new epoch")
}

func TestBurnSentinelRewardPoolOverflow_NoopWhenUnderCap(t *testing.T) {
	f := initFixture(t)
	// Pool starts empty and is well below MaxSentinelRewardPool; nothing to burn.
	require.NoError(t, f.keeper.BurnSentinelRewardPoolOverflow(f.ctx))
}

func TestExpireTags_RemovesExpired(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Tag whose expiration is in the past.
	require.NoError(t, k.SetTag(ctx, types.Tag{Name: "stale", ExpirationIndex: 100}))
	// Tag whose expiration is in the future — must survive.
	require.NoError(t, k.SetTag(ctx, types.Tag{Name: "fresh", ExpirationIndex: 10_000}))
	// Tag with no expiration — must survive.
	require.NoError(t, k.SetTag(ctx, types.Tag{Name: "permanent"}))
	// Reserved tags are skipped even when expired.
	require.NoError(t, k.SetTag(ctx, types.Tag{Name: "reserved_expired", ExpirationIndex: 100}))
	require.NoError(t, k.SetReservedTag(ctx, types.ReservedTag{Name: "reserved_expired"}))

	const now = 500
	require.NoError(t, k.ExpireTags(ctx, now))

	exists, err := k.TagExists(ctx, "stale")
	require.NoError(t, err)
	require.False(t, exists, "expired tag should be removed")

	exists, err = k.TagExists(ctx, "fresh")
	require.NoError(t, err)
	require.True(t, exists)

	exists, err = k.TagExists(ctx, "permanent")
	require.NoError(t, err)
	require.True(t, exists)

	exists, err = k.TagExists(ctx, "reserved_expired")
	require.NoError(t, err)
	require.True(t, exists, "reserved tags are protected from expiry GC")
}
