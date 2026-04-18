package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestSalvationCounters_GetMissingMember(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	epochSalvations, lastEpoch, err := k.GetSalvationCounters(f.ctx, sdk.AccAddress([]byte("ghost")).String())
	require.NoError(t, err, "absent member should report zero rather than error")
	require.Equal(t, uint32(0), epochSalvations)
	require.Equal(t, int64(0), lastEpoch)
}

func TestSalvationCounters_UpdateAndRead(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	addr := sdk.AccAddress([]byte("member01")).String()
	zero := math.ZeroInt()
	require.NoError(t, k.Member.Set(ctx, addr, types.Member{
		Address:        addr,
		DreamBalance:   &zero,
		StakedDream:    &zero,
		LifetimeEarned: &zero,
		LifetimeBurned: &zero,
	}))

	require.NoError(t, k.UpdateSalvationCounters(ctx, addr, 2, 42))

	got, got2, err := k.GetSalvationCounters(ctx, addr)
	require.NoError(t, err)
	require.Equal(t, uint32(2), got)
	require.Equal(t, int64(42), got2)

	// Update is a full overwrite, not an accumulator.
	require.NoError(t, k.UpdateSalvationCounters(ctx, addr, 0, 0))
	got, got2, err = k.GetSalvationCounters(ctx, addr)
	require.NoError(t, err)
	require.Zero(t, got)
	require.Zero(t, got2)
}

func TestSalvationCounters_UpdateOnMissingMemberIsNoop(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	// Updating a non-existent member silently succeeds (documented behavior).
	err := k.UpdateSalvationCounters(f.ctx, sdk.AccAddress([]byte("ghost")).String(), 5, 100)
	require.NoError(t, err)
}

