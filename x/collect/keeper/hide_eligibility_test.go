package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

// TestHideContent_UnbondingSentinelEligibility pins the gap closed by moving
// eligibility into rep's shared EligibleForRole: collect previously accepted
// any non-DEMOTED bonded role, letting an UNBONDING sentinel whose staying
// bond (current - pending_unbond) no longer covered the role minimum keep
// hiding collect content while forum already rejected them.
func TestHideContent_UnbondingSentinelEligibility(t *testing.T) {
	minBond := math.NewInt(500_000_000) // 500 DREAM role minimum

	setup := func(t *testing.T, pendingUnbond math.Int) (*testFixture, uint64) {
		t.Helper()
		f := initTestFixture(t)
		denyCouncil(f)
		f.setBlockHeight(100)

		require.NoError(t, f.repKeeper.SetBondedRoleConfig(f.ctx, reptypes.BondedRoleConfig{
			RoleType: reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL,
			MinBond:  minBond.String(),
		}))

		key := mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)
		br := f.repKeeper.bondedRoles[key]
		br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING
		br.PendingUnbondAmount = pendingUnbond.String()
		f.repKeeper.bondedRoles[key] = br

		return f, f.createCollection(t, f.owner)
	}

	hide := func(f *testFixture, collID uint64) error {
		_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
			Creator:    f.sentinel,
			TargetId:   collID,
			TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
		})
		return err
	}

	t.Run("staying bond below minimum rejected", func(t *testing.T) {
		// Fixture default bond is 1000 DREAM; withdraw all but 499.
		f, collID := setup(t, math.NewInt(1_000_000_000).Sub(minBond).Add(math.NewInt(1)))
		err := hide(f, collID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an active forum sentinel")
	})

	t.Run("staying bond at minimum allowed", func(t *testing.T) {
		f, collID := setup(t, math.NewInt(1_000_000_000).Sub(minBond))
		require.NoError(t, hide(f, collID))
	})
}
