package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"

	"sparkdream/x/rep/types"
)

// bondedRoleKey mirrors the keeper's internal composite-key helper so tests
// can seed records directly.
func bondedRoleKey(roleType types.RoleType, addr string) collections.Pair[int32, string] {
	return collections.Join(int32(roleType), addr)
}

// setMemberWithStaked seeds a Member record with the given spendable balance
// and staked (locked) DREAM balances so LockDREAM / UnlockDREAM / BurnDREAM
// calls have the right preconditions during bonded-role slashes.
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

// seedBondedRoleConfig writes a minimal config for roleType so BondRole /
// UnbondRole / status transitions have thresholds to consult.
func seedBondedRoleConfig(t *testing.T, f *fixture, roleType types.RoleType, minBond, demotionThreshold int64) {
	t.Helper()
	require.NoError(t, f.keeper.SetBondedRoleConfig(f.ctx, types.BondedRoleConfig{
		RoleType:          roleType,
		MinBond:           math.NewInt(minBond).String(),
		DemotionThreshold: math.NewInt(demotionThreshold).String(),
		DemotionCooldown:  604800,
	}))
}

func TestBondedRole_ValidateRoleType(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	addr := sdk.AccAddress([]byte("role1")).String()

	_, err := k.IsBondedRole(f.ctx, types.RoleType_ROLE_TYPE_UNSPECIFIED, addr)
	require.ErrorIs(t, err, types.ErrInvalidRoleType)
	_, err = k.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_UNSPECIFIED, addr)
	require.ErrorIs(t, err, types.ErrInvalidRoleType)
}

func TestBondedRole_IsAndGet(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	addr := sdk.AccAddress([]byte("sentinel1")).String()

	yes, err := k.IsBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr)
	require.NoError(t, err)
	require.False(t, yes)

	_, err = k.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr)
	require.ErrorIs(t, err, types.ErrBondedRoleNotFound)

	// Seed a record directly.
	require.NoError(t, k.BondedRoles.Set(f.ctx, bondedRoleKey(types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr), types.BondedRole{
		Address:            addr,
		RoleType:           types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		BondStatus:         types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		CurrentBond:        "1000",
		TotalCommittedBond: "0",
	}))

	yes, err = k.IsBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr)
	require.NoError(t, err)
	require.True(t, yes)

	br, err := k.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr)
	require.NoError(t, err)
	require.Equal(t, addr, br.Address)
	require.Equal(t, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, br.RoleType)
}

func TestBondedRole_ReserveReleaseSlash(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	role := types.RoleType_ROLE_TYPE_COLLECT_CURATOR
	addr := sdk.AccAddress([]byte("curator1"))

	// No record yet: available bond is zero, reserve fails.
	avail, err := k.GetAvailableBond(f.ctx, role, addr.String())
	require.NoError(t, err)
	require.True(t, avail.IsZero())
	require.ErrorIs(t, k.ReserveBond(f.ctx, role, addr.String(), math.NewInt(1)), types.ErrBondedRoleNotFound)

	// Set up a member with 500 staked and 500 balance so SlashBond can
	// unlock (staked→balance) then burn from balance.
	setMemberWithStaked(t, f, addr, math.NewInt(500), math.NewInt(500))
	require.NoError(t, k.BondedRoles.Set(f.ctx, bondedRoleKey(role, addr.String()), types.BondedRole{
		Address:            addr.String(),
		RoleType:           role,
		BondStatus:         types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		CurrentBond:        "500",
		TotalCommittedBond: "0",
	}))
	seedBondedRoleConfig(t, f, role, 400, 100)

	avail, err = k.GetAvailableBond(f.ctx, role, addr.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500), avail)

	// Reserve 200 → available 300.
	require.NoError(t, k.ReserveBond(f.ctx, role, addr.String(), math.NewInt(200)))
	avail, _ = k.GetAvailableBond(f.ctx, role, addr.String())
	require.Equal(t, math.NewInt(300), avail)

	// Over-reserve fails.
	require.ErrorIs(t, k.ReserveBond(f.ctx, role, addr.String(), math.NewInt(400)), types.ErrInsufficientBond)

	// Release 50 → available 350.
	require.NoError(t, k.ReleaseBond(f.ctx, role, addr.String(), math.NewInt(50)))
	avail, _ = k.GetAvailableBond(f.ctx, role, addr.String())
	require.Equal(t, math.NewInt(350), avail)

	// Slash 200 → current 300, committed saturates to 0, available 300.
	require.NoError(t, k.SlashBond(f.ctx, role, addr.String(), math.NewInt(200), "test"))
	br, err := k.GetBondedRole(f.ctx, role, addr.String())
	require.NoError(t, err)
	require.Equal(t, "300", br.CurrentBond)
	require.Equal(t, "0", br.TotalCommittedBond)
	// 300 >= 100 (demotion_threshold) but < 400 (min_bond) → RECOVERY.
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY, br.BondStatus)
}

// TestBondedRole_ReserveExcludesPendingUnbond proves Fix 1: bond pledged to
// leave (pending_unbond_amount) is not available to back new reservations.
// available = current - committed - pending.
func TestBondedRole_ReserveExcludesPendingUnbond(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	role := types.RoleType_ROLE_TYPE_FORUM_SENTINEL
	addr := sdk.AccAddress([]byte("unbonder1"))

	setMemberWithStaked(t, f, addr, math.NewInt(700), math.NewInt(700))
	// 700 bonded, 100 leaving → 600 staying, none committed.
	require.NoError(t, k.BondedRoles.Set(f.ctx, bondedRoleKey(role, addr.String()), types.BondedRole{
		Address:             addr.String(),
		RoleType:            role,
		BondStatus:          types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING,
		CurrentBond:         "700",
		TotalCommittedBond:  "0",
		PendingUnbondAmount: "100",
	}))

	// Available excludes the pending withdrawal.
	avail, err := k.GetAvailableBond(f.ctx, role, addr.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(600), avail)

	// A reservation that fits the staying bond succeeds.
	require.NoError(t, k.ReserveBond(f.ctx, role, addr.String(), math.NewInt(600)))

	// Nothing left for another reservation, even though current_bond is 700:
	// the extra 100 is on its way out.
	require.ErrorIs(t, k.ReserveBond(f.ctx, role, addr.String(), math.NewInt(1)), types.ErrInsufficientBond)
}

// TestBondedRole_MatureDefersCommittedBond proves Fix 2: unbond maturity never
// unlocks earmarked (committed) bond. With committed > current - pending the
// release is capped and the remainder stays pending for a later block.
func TestBondedRole_MatureDefersCommittedBond(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	role := types.RoleType_ROLE_TYPE_FORUM_SENTINEL
	addr := sdk.AccAddress([]byte("unbonder2"))

	setMemberWithStaked(t, f, addr, math.NewInt(700), math.NewInt(700))
	seedBondedRoleConfig(t, f, role, 400, 100)
	// 700 bonded, 500 committed to an outstanding action, 300 queued to unbond.
	// Only current - committed = 200 may be released; 100 stays pending.
	require.NoError(t, k.BondedRoles.Set(f.ctx, bondedRoleKey(role, addr.String()), types.BondedRole{
		Address:              addr.String(),
		RoleType:             role,
		BondStatus:           types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING,
		CurrentBond:          "700",
		TotalCommittedBond:   "500",
		PendingUnbondAmount:  "300",
		UnbondCompletionTime: 1000,
	}))

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	matured := sdkCtx.WithBlockTime(time.Unix(1001, 0))
	require.NoError(t, k.MatureUnbonds(matured))

	br, err := k.GetBondedRole(matured, role, addr.String())
	require.NoError(t, err)
	// Released 200, current 500, 100 still pending, never unlocked the committed.
	require.Equal(t, "500", br.CurrentBond)
	require.Equal(t, "100", br.PendingUnbondAmount)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING, br.BondStatus)
	// Invariant: current_bond >= total_committed_bond never violated.
	require.Equal(t, "500", br.TotalCommittedBond)
}

func TestBondedRole_SlashCapsAtCurrent(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	role := types.RoleType_ROLE_TYPE_FORUM_SENTINEL
	addr := sdk.AccAddress([]byte("guard1"))

	setMemberWithStaked(t, f, addr, math.NewInt(200), math.NewInt(200))
	require.NoError(t, k.BondedRoles.Set(f.ctx, bondedRoleKey(role, addr.String()), types.BondedRole{
		Address:     addr.String(),
		RoleType:    role,
		BondStatus:  types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		CurrentBond: "200", TotalCommittedBond: "0",
	}))

	// Request slash greater than current_bond: capped at current_bond.
	require.NoError(t, k.SlashBond(f.ctx, role, addr.String(), math.NewInt(500), "cap"))
	br, _ := k.GetBondedRole(f.ctx, role, addr.String())
	require.Equal(t, "0", br.CurrentBond)
}

func TestBondedRole_MultiRoleSameAddress(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	addr := sdk.AccAddress([]byte("multi1")).String()

	// Seed same address under both role types with different bonds.
	require.NoError(t, k.BondedRoles.Set(f.ctx, bondedRoleKey(types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr), types.BondedRole{
		Address: addr, RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL, CurrentBond: "1000", TotalCommittedBond: "0",
	}))
	require.NoError(t, k.BondedRoles.Set(f.ctx, bondedRoleKey(types.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr), types.BondedRole{
		Address: addr, RoleType: types.RoleType_ROLE_TYPE_COLLECT_CURATOR, CurrentBond: "500", TotalCommittedBond: "0",
	}))

	// Each role sees its own record.
	s, err := k.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr)
	require.NoError(t, err)
	require.Equal(t, "1000", s.CurrentBond)

	c, err := k.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr)
	require.NoError(t, err)
	require.Equal(t, "500", c.CurrentBond)

	// Available bond computed independently.
	avail, _ := k.GetAvailableBond(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr)
	require.Equal(t, math.NewInt(1000), avail)
	avail, _ = k.GetAvailableBond(f.ctx, types.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr)
	require.Equal(t, math.NewInt(500), avail)
}

func TestBondedRole_SetAndGetConfig(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	// Defaults from DefaultBondedRoleConfigs are seeded by InitGenesis.
	got, err := k.GetBondedRoleConfig(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL)
	require.NoError(t, err)
	require.Equal(t, "1000", got.MinBond)

	// Overwrite the default via SetBondedRoleConfig (module write-through path).
	cfg := types.BondedRoleConfig{
		RoleType:          types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		MinBond:           "2500",
		MinRepTier:        4,
		MinTrustLevel:     "",
		DemotionCooldown:  86400,
		DemotionThreshold: "700",
	}
	require.NoError(t, k.SetBondedRoleConfig(f.ctx, cfg))
	got, err = k.GetBondedRoleConfig(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL)
	require.NoError(t, err)
	require.Equal(t, cfg.MinBond, got.MinBond)
	require.Equal(t, cfg.MinRepTier, got.MinRepTier)
	require.Equal(t, cfg.DemotionThreshold, got.DemotionThreshold)
	require.Equal(t, int64(86400), got.DemotionCooldown)

	// Write-through normalizes empty numeric fields to "0".
	empty := types.BondedRoleConfig{RoleType: types.RoleType_ROLE_TYPE_COLLECT_CURATOR}
	require.NoError(t, k.SetBondedRoleConfig(f.ctx, empty))
	got, _ = k.GetBondedRoleConfig(f.ctx, types.RoleType_ROLE_TYPE_COLLECT_CURATOR)
	require.Equal(t, "0", got.MinBond)
	require.Equal(t, "0", got.DemotionThreshold)

	// Invalid role_type rejected.
	require.ErrorIs(t, k.SetBondedRoleConfig(f.ctx, types.BondedRoleConfig{RoleType: types.RoleType_ROLE_TYPE_UNSPECIFIED}), types.ErrInvalidRoleType)

	// Unparseable numeric fields rejected.
	require.ErrorIs(t, k.SetBondedRoleConfig(f.ctx, types.BondedRoleConfig{
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL, MinBond: "not-a-number",
	}), types.ErrInvalidAmount)
}

func TestBondedRole_SetBondStatus(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	role := types.RoleType_ROLE_TYPE_FORUM_SENTINEL
	addr := sdk.AccAddress([]byte("demo1")).String()

	// Missing record rejected.
	require.ErrorIs(t,
		k.SetBondStatus(f.ctx, role, addr, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, 1234),
		types.ErrBondedRoleNotFound)

	require.NoError(t, k.BondedRoles.Set(f.ctx, bondedRoleKey(role, addr), types.BondedRole{
		Address: addr, RoleType: role, BondStatus: types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		CurrentBond: "1000", TotalCommittedBond: "0",
	}))
	require.NoError(t,
		k.SetBondStatus(f.ctx, role, addr, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, 999))
	br, _ := k.GetBondedRole(f.ctx, role, addr)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, br.BondStatus)
	require.Equal(t, int64(999), br.DemotionCooldownUntil)
}

func TestBondedRole_RecordActivity(t *testing.T) {
	params := types.DefaultParams()
	params.EpochBlocks = 10
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	role := types.RoleType_ROLE_TYPE_FORUM_SENTINEL
	addr := sdk.AccAddress([]byte("active1")).String()

	// No-op on missing record.
	require.NoError(t, k.RecordActivity(f.ctx, role, addr))

	// Seed a stale ConsecutiveInactiveEpochs counter.
	require.NoError(t, k.BondedRoles.Set(f.ctx, bondedRoleKey(role, addr), types.BondedRole{
		Address: addr, RoleType: role, BondStatus: types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		CurrentBond: "1000", TotalCommittedBond: "0",
		LastActiveEpoch:           0,
		ConsecutiveInactiveEpochs: 5,
	}))

	// Advance to epoch 5 (block 55 / 10) so RecordActivity has work to do.
	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(55)
	require.NoError(t, k.RecordActivity(sdkCtx, role, addr))
	br, _ := k.GetBondedRole(sdkCtx, role, addr)
	require.Equal(t, uint64(0), br.ConsecutiveInactiveEpochs)
	require.Equal(t, int64(5), br.LastActiveEpoch)
}
