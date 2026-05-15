package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// bondSentinelForUnbond is a small fixture helper for the unbond tests. Uses
// the shared seedRoleCandidate helper from msg_server_bond_role_test.go.
// The default FORUM_SENTINEL config seeded by DefaultBondedRoleConfigs has
// UnbondCooldown=1209600 (14 days), so unbond goes through the queued path.
func bondSentinelForUnbond(t *testing.T, f *fixture, addr sdk.AccAddress, amount string) {
	t.Helper()
	srv := keeper.NewMsgServerImpl(f.keeper)
	seedRoleCandidate(t, f, addr, math.NewInt(5_000), "250.0", types.TrustLevel_TRUST_LEVEL_NEW)
	_, err := srv.BondRole(f.ctx, &types.MsgBondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   amount,
	})
	require.NoError(t, err)
}

// setUnbondCooldown overrides the seeded BondedRoleConfig's UnbondCooldown for
// tests that exercise the legacy immediate-unlock path (cooldown=0).
func setUnbondCooldown(t *testing.T, f *fixture, roleType types.RoleType, seconds int64) {
	t.Helper()
	cfg, err := f.keeper.GetBondedRoleConfig(f.ctx, roleType)
	require.NoError(t, err)
	cfg.UnbondCooldown = seconds
	require.NoError(t, f.keeper.SetBondedRoleConfig(f.ctx, cfg))
}

// TestUnbondRole_QueuesAndMatures covers the primary queued-unbond happy path:
// unbond queues, status flips to UNBONDING, DREAM stays locked, and the
// EndBlocker maturity (simulated here via direct MatureUnbonds + clock
// advance) unlocks the pending amount and flips status to DEMOTED.
func TestUnbondRole_QueuesAndMatures(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelU"))
	bondSentinelForUnbond(t, f, addr, "2000")

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "500",
	})
	require.NoError(t, err)

	br, _ := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING, br.BondStatus)
	require.Equal(t, "500", br.PendingUnbondAmount)
	require.Equal(t, "2000", br.CurrentBond, "DREAM stays locked during cooldown")
	require.Greater(t, br.UnbondCompletionTime, int64(0))

	// Member's staked DREAM is unchanged — still locked.
	mem, _ := f.keeper.Member.Get(f.ctx, addr.String())
	require.Equal(t, "2000", mem.StakedDream.String())

	// Advance the clock past completion_time and mature.
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	matured := sdkCtx.WithBlockTime(time.Unix(br.UnbondCompletionTime+1, 0))
	require.NoError(t, f.keeper.MatureUnbonds(matured))

	br, _ = f.keeper.GetBondedRole(matured, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	// Partial unbond: remaining 1500 >= min_bond=1000, so maturity keeps the
	// holder NORMAL — they reduced their stake but stayed an active role.
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, br.BondStatus)
	require.Equal(t, "0", br.PendingUnbondAmount)
	require.Equal(t, int64(0), br.UnbondCompletionTime)
	require.Equal(t, "1500", br.CurrentBond)
	require.Equal(t, int64(0), br.DemotionCooldownUntil, "no demotion cooldown when staying active")

	mem, _ = f.keeper.Member.Get(matured, addr.String())
	require.Equal(t, "1500", mem.StakedDream.String(), "DREAM unlocked back to staked balance")
}

// TestUnbondRole_FullUnbondMaturityDemotes: a full unbond drains current_bond
// below demotion_threshold, so maturity transitions to DEMOTED and starts the
// demotion_cooldown gating re-bonding.
func TestUnbondRole_FullUnbondMaturityDemotes(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelFull"))
	bondSentinelForUnbond(t, f, addr, "2000")

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "2000",
	})
	require.NoError(t, err)

	br, _ := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	matured := sdkCtx.WithBlockTime(time.Unix(br.UnbondCompletionTime+1, 0))
	require.NoError(t, f.keeper.MatureUnbonds(matured))

	br, _ = f.keeper.GetBondedRole(matured, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, br.BondStatus,
		"full unbond drops bond below demotion_threshold → DEMOTED")
	require.Equal(t, "0", br.CurrentBond)
	require.Greater(t, br.DemotionCooldownUntil, int64(0),
		"demotion cooldown gates re-bonding after full exit")
}

// TestUnbondRole_RejectsSecondUnbondWhileInFlight: once UNBONDING, a second
// MsgUnbondRole is refused. State machine stays linear.
func TestUnbondRole_RejectsSecondUnbondWhileInFlight(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelD"))
	bondSentinelForUnbond(t, f, addr, "2000")

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "500",
	})
	require.NoError(t, err)

	_, err = srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "100",
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

// TestBondRole_RejectsTopUpWhileUnbonding: cannot bond more while an unbond
// is draining. Keeps the state machine linear.
func TestBondRole_RejectsTopUpWhileUnbonding(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelTop"))
	bondSentinelForUnbond(t, f, addr, "2000")

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "500",
	})
	require.NoError(t, err)

	_, err = srv.BondRole(f.ctx, &types.MsgBondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "1000",
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

// TestSlashBond_DuringUnbondingPreservesStatusAndCapsPending: a slash during
// the cooldown drains current_bond AND caps pending_unbond_amount at the new
// current_bond. Status stays UNBONDING — only MatureUnbonds flips it.
func TestSlashBond_DuringUnbondingPreservesStatusAndCapsPending(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelSL"))
	bondSentinelForUnbond(t, f, addr, "2000")

	// Unbond all 2000.
	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "2000",
	})
	require.NoError(t, err)

	br, _ := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, "2000", br.PendingUnbondAmount)

	// Slash 500 mid-cooldown. current_bond drops, pending caps to 1500.
	require.NoError(t, f.keeper.SlashBond(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		addr.String(), math.NewInt(500), "test slash during unbond"))

	br, _ = f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING, br.BondStatus,
		"status stays UNBONDING through slashes — only MatureUnbonds flips")
	require.Equal(t, "1500", br.CurrentBond)
	require.Equal(t, "1500", br.PendingUnbondAmount, "pending capped at new current_bond")

	// Mature: holder gets back 1500 (the slashed amount is gone).
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	matured := sdkCtx.WithBlockTime(time.Unix(br.UnbondCompletionTime+1, 0))
	require.NoError(t, f.keeper.MatureUnbonds(matured))

	br, _ = f.keeper.GetBondedRole(matured, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, br.BondStatus)
	require.Equal(t, "0", br.CurrentBond)
	require.Equal(t, "0", br.PendingUnbondAmount)
}

// TestUnbondRole_LegacyImmediateUnlock covers the path where UnbondCooldown=0:
// DREAM unlocks immediately and status is recomputed from the new bond.
// Preserves the pre-cooldown behavior for tests and roles that opt out.
func TestUnbondRole_LegacyImmediateUnlock(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelL"))
	bondSentinelForUnbond(t, f, addr, "2000")

	// Disable cooldown to opt into legacy immediate-unlock path.
	setUnbondCooldown(t, f, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, 0)

	// Partial unbond: 2000 → 1500.
	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "500",
	})
	require.NoError(t, err)

	br, _ := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, "1500", br.CurrentBond)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, br.BondStatus)

	mem, _ := f.keeper.Member.Get(f.ctx, addr.String())
	require.Equal(t, "1500", mem.StakedDream.String())
}

// TestUnbondRole_LegacyTransitionsToRecoveryThenDemoted verifies the
// successive-partial-unbond flow under UnbondCooldown=0 still walks
// NORMAL → RECOVERY → DEMOTED correctly.
func TestUnbondRole_LegacyTransitionsToRecoveryThenDemoted(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelLD"))
	bondSentinelForUnbond(t, f, addr, "2000")
	setUnbondCooldown(t, f, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, 0)

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "1100",
	})
	require.NoError(t, err)
	br, _ := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, "900", br.CurrentBond)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY, br.BondStatus)
	require.Zero(t, br.DemotionCooldownUntil)

	_, err = srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "500",
	})
	require.NoError(t, err)
	br, _ = f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, "400", br.CurrentBond)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, br.BondStatus)
	require.Greater(t, br.DemotionCooldownUntil, int64(0))
}

func TestUnbondRole_CannotExceedAvailable(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelC"))
	bondSentinelForUnbond(t, f, addr, "2000")

	require.NoError(t, f.keeper.ReserveBond(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String(), math.NewInt(1200)))

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "1500",
	})
	require.ErrorIs(t, err, types.ErrInsufficientBond)
}

func TestUnbondRole_MissingRecord(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	addr := sdk.AccAddress([]byte("ghost"))

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "100",
	})
	require.ErrorIs(t, err, types.ErrBondedRoleNotFound)
}

func TestUnbondRole_InvalidRoleType(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	addr := sdk.AccAddress([]byte("any"))

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_UNSPECIFIED,
		Amount:   "100",
	})
	require.ErrorIs(t, err, types.ErrInvalidRoleType)
}

// TestMatureUnbonds_NoOpBeforeCompletionTime: records whose unbond_completion_time
// is in the future are left untouched (DREAM stays locked, status stays
// UNBONDING). Ensures MatureUnbonds can run every block without prematurely
// finalizing pending unbonds.
func TestMatureUnbonds_NoOpBeforeCompletionTime(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelM"))
	bondSentinelForUnbond(t, f, addr, "2000")

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "500",
	})
	require.NoError(t, err)

	br, _ := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING, br.BondStatus)

	// Run maturity at current block time (still before completion).
	require.NoError(t, f.keeper.MatureUnbonds(f.ctx))

	br, _ = f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING, br.BondStatus, "still UNBONDING — cooldown not elapsed")
	require.Equal(t, "500", br.PendingUnbondAmount)
	require.Equal(t, "2000", br.CurrentBond)
}

// TestMatureUnbonds_HandlesMultipleRecords: one MatureUnbonds pass finalizes
// all records whose cooldown has elapsed, leaves the others alone.
func TestMatureUnbonds_HandlesMultipleRecords(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr1 := sdk.AccAddress([]byte("sentinelM1"))
	addr2 := sdk.AccAddress([]byte("sentinelM2"))
	bondSentinelForUnbond(t, f, addr1, "2000")
	bondSentinelForUnbond(t, f, addr2, "2000")

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator: addr1.String(), RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL, Amount: "500",
	})
	require.NoError(t, err)
	_, err = srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator: addr2.String(), RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL, Amount: "1000",
	})
	require.NoError(t, err)

	br1, _ := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr1.String())

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	matured := sdkCtx.WithBlockTime(time.Unix(br1.UnbondCompletionTime+1, 0))
	require.NoError(t, f.keeper.MatureUnbonds(matured))

	br1, _ = f.keeper.GetBondedRole(matured, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr1.String())
	br2, _ := f.keeper.GetBondedRole(matured, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr2.String())
	// Both records are matured in the same pass. Both partial unbonds leave
	// current_bond ≥ min_bond=1000, so status stays NORMAL.
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, br1.BondStatus)
	require.Equal(t, "1500", br1.CurrentBond)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, br2.BondStatus)
	require.Equal(t, "1000", br2.CurrentBond)
	require.Equal(t, "0", br1.PendingUnbondAmount, "pending cleared on maturity")
	require.Equal(t, "0", br2.PendingUnbondAmount, "pending cleared on maturity")
}

// TestMatureUnbonds_FullySlashed: if mid-cooldown slashes drain current_bond
// to zero, MatureUnbonds still completes — holder gets nothing back, status
// flips to DEMOTED, demotion_cooldown starts.
func TestMatureUnbonds_FullySlashed(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelMS"))
	bondSentinelForUnbond(t, f, addr, "2000")

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator: addr.String(), RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL, Amount: "2000",
	})
	require.NoError(t, err)

	// Slash everything during cooldown.
	require.NoError(t, f.keeper.SlashBond(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		addr.String(), math.NewInt(2000), "full slash"))

	br, _ := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING, br.BondStatus)
	require.Equal(t, "0", br.CurrentBond)
	require.Equal(t, "0", br.PendingUnbondAmount)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	matured := sdkCtx.WithBlockTime(time.Unix(br.UnbondCompletionTime+1, 0))
	require.NoError(t, f.keeper.MatureUnbonds(matured))

	br, _ = f.keeper.GetBondedRole(matured, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, br.BondStatus)
	require.Equal(t, "0", br.CurrentBond)
	require.Greater(t, br.DemotionCooldownUntil, int64(0), "demotion cooldown starts even on zero payout")
}

func TestUnbondRole_InvalidAmount(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	addr := sdk.AccAddress([]byte("any"))

	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "not-a-number",
	})
	require.ErrorIs(t, err, types.ErrInvalidAmount)

	_, err = srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "0",
	})
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}
