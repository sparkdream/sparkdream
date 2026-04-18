package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// bondFresh runs the full bond path so the unbond test starts from realistic
// keeper state (sentinel record + locked DREAM + member balance).
func bondFresh(t *testing.T, f *fixture, srv types.MsgServer, addr sdk.AccAddress, amount string) {
	t.Helper()
	_, err := srv.BondSentinel(f.ctx, &types.MsgBondSentinel{
		Creator: addr.String(),
		Amount:  amount,
	})
	require.NoError(t, err)
}

func TestUnbondSentinel_HappyPath(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentBBB"))
	seedSentinelCandidate(t, f, addr, math.NewInt(10_000))
	bondFresh(t, f, srv, addr, "3000")

	_, err := srv.UnbondSentinel(f.ctx, &types.MsgUnbondSentinel{
		Creator: addr.String(),
		Amount:  "1000",
	})
	require.NoError(t, err)

	sa, err := f.keeper.GetSentinel(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, "2000", sa.CurrentBond)
	require.Equal(t, types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL, sa.BondStatus)

	// DREAM back to unlocked.
	mem, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, "2000", mem.StakedDream.String())
}

func TestUnbondSentinel_CrossingRecoveryFloorTriggersDemotionCooldown(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentBBB"))
	seedSentinelCandidate(t, f, addr, math.NewInt(10_000))
	bondFresh(t, f, srv, addr, "2000")

	// Unbond most of it so newBond drops below DefaultSentinelDemotionThreshold (500).
	_, err := srv.UnbondSentinel(f.ctx, &types.MsgUnbondSentinel{
		Creator: addr.String(),
		Amount:  "1900", // newBond = 100, below 500 floor
	})
	require.NoError(t, err)

	sa, err := f.keeper.GetSentinel(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED, sa.BondStatus)
	require.Greater(t, sa.DemotionCooldownUntil, int64(0), "demotion cooldown must be set when crossing the floor")
}

func TestUnbondSentinel_RejectsCommittedBond(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentBBB"))
	seedSentinelCandidate(t, f, addr, math.NewInt(10_000))
	bondFresh(t, f, srv, addr, "3000")

	// Simulate 2000 of the 3000 being reserved for pending moderation actions.
	sa, err := f.keeper.GetSentinel(f.ctx, addr.String())
	require.NoError(t, err)
	sa.TotalCommittedBond = "2000"
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, addr.String(), sa))

	_, err = srv.UnbondSentinel(f.ctx, &types.MsgUnbondSentinel{
		Creator: addr.String(),
		Amount:  "2000", // only 1000 is available
	})
	require.ErrorIs(t, err, types.ErrInsufficientSentinelBond)
}

func TestUnbondSentinel_RejectsOverdrawn(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentBBB"))
	seedSentinelCandidate(t, f, addr, math.NewInt(10_000))
	bondFresh(t, f, srv, addr, "2000")

	_, err := srv.UnbondSentinel(f.ctx, &types.MsgUnbondSentinel{
		Creator: addr.String(),
		Amount:  "9999",
	})
	require.ErrorIs(t, err, types.ErrInsufficientSentinelBond)
}

func TestUnbondSentinel_RejectsInvalidInputs(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	// Not a sentinel.
	ghost := sdk.AccAddress([]byte("ghost"))
	_, err := srv.UnbondSentinel(f.ctx, &types.MsgUnbondSentinel{
		Creator: ghost.String(),
		Amount:  "100",
	})
	require.ErrorIs(t, err, types.ErrSentinelNotFound)

	// Sentinel with a zero amount.
	addr := sdk.AccAddress([]byte("zeroamt"))
	seedSentinelCandidate(t, f, addr, math.NewInt(10_000))
	bondFresh(t, f, srv, addr, "2000")
	_, err = srv.UnbondSentinel(f.ctx, &types.MsgUnbondSentinel{
		Creator: addr.String(),
		Amount:  "0",
	})
	require.ErrorIs(t, err, types.ErrInvalidAmount)

	// Bad address.
	_, err = srv.UnbondSentinel(f.ctx, &types.MsgUnbondSentinel{
		Creator: "not-an-address",
		Amount:  "100",
	})
	require.Error(t, err)
}
