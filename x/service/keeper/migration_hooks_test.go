package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// REP-R-02(d): a live operator on the old address vetoes only a KEY
// migration. On RECOVERY / COUNCIL a thief holding the old key could
// register one just to block recovery, so the hook does not veto there;
// finalization forces the operator into UNBONDING, and the canonical address
// collects the matured exit bond through the tombstone sweep. A SLASHED
// record is terminal and never vetoes.
func TestMigrationHooks_OperatorPathAware(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	bond := math.NewInt(2_000_000)
	op := f.seedActiveOperator(t, testOperator1, testController, bond)
	h := keeper.NewMigrationHooks(&f.keeper)
	from, to := testOperator1Addr, testOperator2Addr

	require.Error(t, h.BeforeMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_KEY))
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_RECOVERY))
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_COUNCIL))
	// The target side vetoes on every path.
	require.Error(t, h.BeforeMemberMigration(f.ctx, to, from, reptypes.MigrationPath_MIGRATION_PATH_RECOVERY))

	// Finalization on the recovery path forces the exit.
	require.NoError(t, h.AfterMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_RECOVERY))
	got, ok := f.keeper.GetOperator(f.ctx, from.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNBONDING, got.Status)

	// Nothing to claim before the unbonding period ends.
	require.NoError(t, f.keeper.ClaimForTombstone(f.ctx, from, to))
	_, ok = f.keeper.GetOperator(f.ctx, from.Bytes(), testServiceType)
	require.True(t, ok)

	// After it, the sweep returns the bond (to the tombstone address; the
	// bank send restriction forwards it to the canonical one) and retires
	// the record.
	f.withBlockHeight(got.UnbondCompleteAt + 1)
	require.NoError(t, f.keeper.ClaimForTombstone(f.ctx, from, to))
	_, ok = f.keeper.GetOperator(f.ctx, from.Bytes(), testServiceType)
	require.False(t, ok)
	require.Len(t, f.bankKeeper.ModToAcctCalls, 1)
	require.True(t, f.bankKeeper.ModToAcctCalls[0].Amt.AmountOf(testBondDenom).Equal(bond))
	_ = op

	// A terminal SLASHED record never vetoes.
	f2 := initFixture(t)
	f2.seedServiceType(t)
	slashed := f2.seedActiveOperator(t, testOperator1, testController, bond)
	slashed.Status = types.OperatorStatus_OPERATOR_STATUS_SLASHED
	require.NoError(t, f2.keeper.PutOperator(f2.ctx, slashed))
	h2 := keeper.NewMigrationHooks(&f2.keeper)
	require.NoError(t, h2.BeforeMemberMigration(f2.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_KEY))
}
