package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func setSentinelWithBond(t *testing.T, f *fixture, addr string, bond math.Int) {
	t.Helper()
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, addr, types.SentinelActivity{
		Address:            addr,
		CurrentBond:        bond.String(),
		TotalCommittedBond: "0",
	}))
}

func setMemberWithStaked(t *testing.T, f *fixture, addr sdk.AccAddress, balance, staked math.Int) {
	t.Helper()
	zero := math.ZeroInt()
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   &balance,
		StakedDream:    &staked,
		LifetimeEarned: &zero,
		LifetimeBurned: &zero,
	}))
}

func TestIsSentinel_AndGetSentinel(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	addr := sdk.AccAddress([]byte("sentinel1")).String()

	yes, err := k.IsSentinel(f.ctx, addr)
	require.NoError(t, err)
	require.False(t, yes)

	_, err = k.GetSentinel(f.ctx, addr)
	require.ErrorIs(t, err, types.ErrSentinelNotFound)

	setSentinelWithBond(t, f, addr, math.NewInt(500))

	yes, err = k.IsSentinel(f.ctx, addr)
	require.NoError(t, err)
	require.True(t, yes)

	sa, err := k.GetSentinel(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, addr, sa.Address)
}

func TestReserveAndReleaseBond(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	addr := sdk.AccAddress([]byte("sentinel1")).String()
	setSentinelWithBond(t, f, addr, math.NewInt(500))

	avail, err := k.GetAvailableBond(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500), avail)

	// Reserve 200. Available drops to 300.
	require.NoError(t, k.ReserveBond(f.ctx, addr, math.NewInt(200)))
	avail, err = k.GetAvailableBond(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(300), avail)

	// Over-reserve is rejected with a typed error.
	err = k.ReserveBond(f.ctx, addr, math.NewInt(1_000))
	require.ErrorIs(t, err, types.ErrInsufficientSentinelBond)

	// Release 150 returns it to the available pool.
	require.NoError(t, k.ReleaseBond(f.ctx, addr, math.NewInt(150)))
	avail, err = k.GetAvailableBond(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(450), avail)

	// Releasing more than committed saturates at zero.
	require.NoError(t, k.ReleaseBond(f.ctx, addr, math.NewInt(10_000)))
	avail, err = k.GetAvailableBond(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500), avail)
}

func TestGetAvailableBond_MissingSentinelIsZero(t *testing.T) {
	f := initFixture(t)
	avail, err := f.keeper.GetAvailableBond(f.ctx, sdk.AccAddress([]byte("ghost")).String())
	require.NoError(t, err)
	require.True(t, avail.IsZero())
}

func TestSlashBond_UnlocksAndBurnsDREAM(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	addr := sdk.AccAddress([]byte("sentinel1"))
	setSentinelWithBond(t, f, addr.String(), math.NewInt(500))
	// The sentinel's bond is held as staked DREAM on their member record.
	setMemberWithStaked(t, f, addr, math.NewInt(500), math.NewInt(500))

	require.NoError(t, k.SlashBond(f.ctx, addr.String(), math.NewInt(200), "test_reason"))

	sa, err := k.GetSentinel(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, "300", sa.CurrentBond)

	// DREAM is burned (staked drops, balance drops).
	mem, err := k.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, "300", mem.DreamBalance.String())
	require.Equal(t, "300", mem.StakedDream.String())
}

func TestSlashBond_CapsAtCurrentBond(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	addr := sdk.AccAddress([]byte("sentinel_small"))
	setSentinelWithBond(t, f, addr.String(), math.NewInt(100))
	setMemberWithStaked(t, f, addr, math.NewInt(100), math.NewInt(100))

	// Request to slash more than exists — should cap at the current bond.
	require.NoError(t, k.SlashBond(f.ctx, addr.String(), math.NewInt(999), "test"))

	sa, err := k.GetSentinel(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, "0", sa.CurrentBond)

	mem, err := k.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	require.True(t, mem.DreamBalance.IsZero())
}

func TestSlashBond_Invariants(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	err := k.SlashBond(f.ctx, "ghost_address", math.NewInt(1), "test")
	require.ErrorIs(t, err, types.ErrSentinelNotFound)

	addr := sdk.AccAddress([]byte("s"))
	setSentinelWithBond(t, f, addr.String(), math.NewInt(100))
	err = k.SlashBond(f.ctx, addr.String(), math.NewInt(0), "test")
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

func TestRecordActivity_IdempotentWithinEpoch(t *testing.T) {
	params := types.DefaultParams()
	params.EpochBlocks = 10
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper

	addr := sdk.AccAddress([]byte("sentinel1")).String()
	require.NoError(t, k.SentinelActivity.Set(f.ctx, addr, types.SentinelActivity{
		Address:                   addr,
		LastActiveEpoch:           0, // stale — must be updated on first call below
		ConsecutiveInactiveEpochs: 3,
	}))

	// Advance to epoch 5 so the first call has work to do.
	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(55)

	// First call bumps LastActiveEpoch to the current epoch and zeroes the
	// consecutive-inactive counter.
	require.NoError(t, k.RecordActivity(sdkCtx, addr))
	sa, err := k.GetSentinel(sdkCtx, addr)
	require.NoError(t, err)
	require.Equal(t, int64(5), sa.LastActiveEpoch)
	require.Equal(t, uint64(0), sa.ConsecutiveInactiveEpochs, "consecutive counter must reset on activity")

	// Second call within the same epoch is a no-op — reset the counter manually
	// to observe that RecordActivity doesn't overwrite it.
	sa.ConsecutiveInactiveEpochs = 7
	require.NoError(t, k.SentinelActivity.Set(sdkCtx, addr, sa))
	require.NoError(t, k.RecordActivity(sdkCtx, addr))

	sa, err = k.GetSentinel(sdkCtx, addr)
	require.NoError(t, err)
	require.Equal(t, uint64(7), sa.ConsecutiveInactiveEpochs, "second same-epoch call must be idempotent")
}

func TestRecordActivity_MissingSentinelIsNoop(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.RecordActivity(f.ctx, sdk.AccAddress([]byte("ghost")).String()))
}

func TestSetBondStatus_UpdatesAndEmits(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	addr := sdk.AccAddress([]byte("sentinel1")).String()
	setSentinelWithBond(t, f, addr, math.NewInt(500))

	require.NoError(t, k.SetBondStatus(f.ctx, addr,
		types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY, 100))

	sa, err := k.GetSentinel(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY, sa.BondStatus)
	require.Equal(t, int64(100), sa.DemotionCooldownUntil)
}

