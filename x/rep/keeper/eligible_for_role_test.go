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
			_ = f.keeper.BondedRoleConfigs.Remove(f.ctx, int32(role))
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
