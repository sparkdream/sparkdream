package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// bondSentinelForUnbond is a small fixture helper for the unbond tests. Uses
// the shared seedRoleCandidate helper from msg_server_bond_role_test.go.
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

func TestUnbondRole_HappyPath(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelU"))
	bondSentinelForUnbond(t, f, addr, "2000")

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

func TestUnbondRole_CannotExceedAvailable(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelC"))
	bondSentinelForUnbond(t, f, addr, "2000")

	// Reserve 1200 → available 800.
	require.NoError(t, f.keeper.ReserveBond(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String(), math.NewInt(1200)))

	// Try to unbond 1500 > available 800.
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

func TestUnbondRole_InvalidAmount(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	addr := sdk.AccAddress([]byte("any"))

	// Non-numeric.
	_, err := srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "not-a-number",
	})
	require.ErrorIs(t, err, types.ErrInvalidAmount)

	// Zero.
	_, err = srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "0",
	})
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

func TestUnbondRole_TransitionsToRecoveryThenDemoted(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelD"))
	// Seeded FORUM_SENTINEL config has min_bond=1000, demotion_threshold=500.
	bondSentinelForUnbond(t, f, addr, "2000")

	// 2000 → 900 drops below min_bond but stays above demotion_threshold → RECOVERY.
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

	// 900 → 400 drops below demotion_threshold → DEMOTED + cooldown set.
	_, err = srv.UnbondRole(f.ctx, &types.MsgUnbondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "500",
	})
	require.NoError(t, err)
	br, _ = f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.Equal(t, "400", br.CurrentBond)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, br.BondStatus)
	require.Greater(t, br.DemotionCooldownUntil, int64(0), "demotion cooldown must be set on crossing into DEMOTED")
}
