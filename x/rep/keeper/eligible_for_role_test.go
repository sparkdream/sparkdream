package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestEligibleForRole pins the shared action-time eligibility gate hoisted
// from forum's eligibleSentinel: NORMAL/RECOVERY eligible outright,
// UNBONDING eligible only while the staying bond (current - pending) covers
// the role's configured min_bond, DEMOTED never eligible.
func TestEligibleForRole(t *testing.T) {
	role := types.RoleType_ROLE_TYPE_CONTENT_SENTINEL
	addr := sdk.AccAddress([]byte("eligible_sentinel___")).String()

	seed := func(t *testing.T, f *fixture, status types.BondedRoleStatus, current, pending int64, withConfig bool) {
		t.Helper()
		if withConfig {
			seedBondedRoleConfig(t, f, role, 500, 100)
		} else {
			// The rep genesis seeds a default sentinel config; remove it so
			// the missing-config fallback path is actually exercised.
			require.NoError(t, f.keeper.BondedRoleConfigs.Remove(f.ctx, int32(role)))
		}
		require.NoError(t, f.keeper.BondedRoles.Set(f.ctx, bondedRoleKey(role, addr), types.BondedRole{
			RoleType:            role,
			Address:             addr,
			CurrentBond:         math.NewInt(current).String(),
			TotalCommittedBond:  "0",
			BondStatus:          status,
			PendingUnbondAmount: math.NewInt(pending).String(),
		}))
	}

	tests := []struct {
		name       string
		status     types.BondedRoleStatus
		current    int64
		pending    int64
		withConfig bool
		expErr     error
	}{
		{
			name:       "NORMAL eligible",
			status:     types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			current:    1000,
			withConfig: true,
		},
		{
			name:       "RECOVERY eligible",
			status:     types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY,
			current:    300,
			withConfig: true,
		},
		{
			name:       "UNBONDING with staying bond at minimum eligible",
			status:     types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING,
			current:    1000,
			pending:    500, // staying = 500 == min
			withConfig: true,
		},
		{
			name:       "UNBONDING with staying bond below minimum rejected",
			status:     types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING,
			current:    1000,
			pending:    501, // staying = 499 < 500
			withConfig: true,
			expErr:     types.ErrRoleUnbondingBelowMin,
		},
		{
			name:    "UNBONDING with missing config falls back to zero minimum",
			status:  types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING,
			current: 100,
			pending: 100, // staying = 0 >= 0
		},
		{
			name:       "DEMOTED rejected",
			status:     types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED,
			current:    1000,
			withConfig: true,
			expErr:     types.ErrRoleDemoted,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			seed(t, f, tc.status, tc.current, tc.pending, tc.withConfig)

			br, err := f.keeper.EligibleForRole(f.ctx, role, addr)
			if tc.expErr != nil {
				require.ErrorIs(t, err, tc.expErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, addr, br.Address)
		})
	}
}

func TestEligibleForRole_NotBonded(t *testing.T) {
	f := initFixture(t)
	_, err := f.keeper.EligibleForRole(f.ctx,
		types.RoleType_ROLE_TYPE_CONTENT_SENTINEL, sdk.AccAddress([]byte("nobody______________")).String())
	require.ErrorIs(t, err, types.ErrBondedRoleNotFound)
}

// The role config's min_age_blocks is enforced here for every role: a bond
// registered fewer blocks ago than the gate cannot act, whatever its
// status; zero means no gate.
func TestEligibleForRole_MinAgeBlocks(t *testing.T) {
	role := types.RoleType_ROLE_TYPE_COLLECT_CURATOR
	addr := sdk.AccAddress([]byte("young_curator_______")).String()
	f := initFixture(t)
	f.ctx = f.ctx.WithBlockHeight(1_000)
	cfg, err := f.keeper.BondedRoleConfigs.Get(f.ctx, int32(role))
	require.NoError(t, err)
	cfg.MinAgeBlocks = 300
	require.NoError(t, f.keeper.BondedRoleConfigs.Set(f.ctx, int32(role), cfg))
	require.NoError(t, f.keeper.BondedRoles.Set(f.ctx, bondedRoleKey(role, addr), types.BondedRole{
		RoleType: role, Address: addr, CurrentBond: "1000", TotalCommittedBond: "0",
		PendingUnbondAmount: "0", BondStatus: types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		RegisteredAt: 900,
	}))

	_, err = f.keeper.EligibleForRole(f.ctx, role, addr)
	require.ErrorIs(t, err, types.ErrRoleTooNew, "100 blocks old, gate 300")

	f.ctx = f.ctx.WithBlockHeight(1_200)
	_, err = f.keeper.EligibleForRole(f.ctx, role, addr)
	require.NoError(t, err, "exactly min_age_blocks old")

	cfg.MinAgeBlocks = 0
	require.NoError(t, f.keeper.BondedRoleConfigs.Set(f.ctx, int32(role), cfg))
	f.ctx = f.ctx.WithBlockHeight(901)
	_, err = f.keeper.EligibleForRole(f.ctx, role, addr)
	require.NoError(t, err, "zero = no gate")
}
