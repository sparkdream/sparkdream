package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// REP-R-12: an inconclusive objection jury (including one that could not be
// seated) no longer cancels a COUNCIL migration and refunds the objector,
// which let a thief holding the old key stall every council migration on a
// chain too thin to seat a jury. The objection escalates to x/gov with the
// bond still locked; gov cancels (objection upheld, bond refunded) or
// re-forces (objection rejected, bond to the new address); undecided past
// MigrationObjectionEscalationWindow it is rejected.
func TestREPR12_InconclusiveObjectionEscalatesToGov(t *testing.T) {
	a := sdk.AccAddress([]byte("escalate-a_________"))
	b := sdk.AccAddress([]byte("escalate-b_________"))
	other := sdk.AccAddress([]byte("escalate-c_________"))
	hr := sdk.AccAddress([]byte("escalate-hr-policy_"))

	// objected forces, accepts and objects to a council migration a -> b and
	// lets the jury deadline pass with no juror seated: the sweep tallies it
	// INCONCLUSIVE.
	objected := func(t *testing.T) (*fixture, uint64, *[]sdk.AccAddress) {
		f := migrationFixture(t)
		seedMigrant(t, f, a, 1)
		paid := &[]sdk.AccAddress{}
		f.bankKeeper.SendCoinsFn = func(_ context.Context, from, to sdk.AccAddress, _ sdk.Coins) error {
			if from.Equals(keeper.AppealBondEscrowAddress()) {
				*paid = append(*paid, to)
			}
			return nil
		}
		id, err := f.keeper.ForceMigration(f.ctx, f.keeper.GetAuthorityString(), a.String(), b.String(), "ipfs://evidence")
		require.NoError(t, err)
		require.NoError(t, f.keeper.AcceptMigration(f.ctx, b.String(), id))
		require.NoError(t, f.keeper.ObjectMigration(f.ctx, a.String(), id, "not me"))
		m, _ := f.keeper.GetMemberMigration(f.ctx, id)
		review, err := f.keeper.GetJuryReview(f.ctx, m.JuryReviewId)
		require.NoError(t, err)
		require.Empty(t, review.Jurors, "the fixture seats no jury")

		advance(f, types.DefaultAppealDeadline+1, 1)
		f.keeper.ProcessMigrations(f.ctx)
		m, _ = f.keeper.GetMemberMigration(f.ctx, id)
		require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_OBJECTED, m.Status, "inconclusive must not cancel")
		require.Equal(t, f.ctx.BlockTime().Unix()+types.MigrationObjectionEscalationWindow, m.EscalatedUntil)
		require.True(t, m.ObjectionBond.IsPositive(), "the bond stays locked")
		require.Empty(t, *paid, "nothing refunded or paid")
		require.False(t, f.keeper.IsTombstoned(f.ctx, a.String()))
		return f, id, paid
	}

	t.Run("a late jury verdict no longer applies", func(t *testing.T) {
		f, id, paid := objected(t)
		m, _ := f.keeper.GetMemberMigration(f.ctx, id)
		review, err := f.keeper.GetJuryReview(f.ctx, m.JuryReviewId)
		require.NoError(t, err)
		require.NoError(t, f.keeper.ResolveMigrationObjectionForTest(f.ctx, review, types.Verdict_VERDICT_UPHOLD_CHALLENGE))
		m, _ = f.keeper.GetMemberMigration(f.ctx, id)
		require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_OBJECTED, m.Status)
		require.Empty(t, *paid)
	})

	t.Run("gov cancel upholds the objection", func(t *testing.T) {
		f, id, paid := objected(t)
		require.NoError(t, f.keeper.CancelMigration(f.ctx, f.keeper.GetAuthorityString(), id))
		m, _ := f.keeper.GetMemberMigration(f.ctx, id)
		require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_CANCELLED, m.Status)
		require.Equal(t, []sdk.AccAddress{a}, *paid, "bond refunded to the objector")
		require.False(t, f.keeper.IsTombstoned(f.ctx, a.String()))
	})

	t.Run("gov re-force rejects the objection", func(t *testing.T) {
		f, id, paid := objected(t)
		// The HR committee filed the migration and cannot overrule the
		// objection; gov cannot redirect it to another address either.
		_, err := f.keeper.ForceMigration(f.ctx, hr.String(), a.String(), b.String(), "")
		require.ErrorIs(t, err, types.ErrMigrationNotAuthorized)
		_, err = f.keeper.ForceMigration(f.ctx, f.keeper.GetAuthorityString(), a.String(), other.String(), "")
		require.ErrorIs(t, err, types.ErrMigrationPending)

		got, err := f.keeper.ForceMigration(f.ctx, f.keeper.GetAuthorityString(), a.String(), b.String(), "")
		require.NoError(t, err)
		require.Equal(t, id, got)
		f.keeper.ProcessMigrations(f.ctx)
		m, _ := f.keeper.GetMemberMigration(f.ctx, id)
		require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_FINALIZED, m.Status, m.FailureReason)
		require.Equal(t, []sdk.AccAddress{b}, *paid, "bond paid to the new address")
		require.True(t, f.keeper.IsTombstoned(f.ctx, a.String()))
	})

	t.Run("an undecided escalation expires as rejected", func(t *testing.T) {
		f, id, paid := objected(t)
		advance(f, types.MigrationObjectionEscalationWindow-1, 1)
		f.keeper.ProcessMigrations(f.ctx)
		m, _ := f.keeper.GetMemberMigration(f.ctx, id)
		require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_OBJECTED, m.Status, "still waiting on gov")

		advance(f, 1, 1)
		f.keeper.ProcessMigrations(f.ctx) // rejects the objection, re-queues
		f.keeper.ProcessMigrations(f.ctx) // finalizes
		m, _ = f.keeper.GetMemberMigration(f.ctx, id)
		require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_FINALIZED, m.Status, m.FailureReason)
		require.Zero(t, m.EscalatedUntil)
		require.Equal(t, []sdk.AccAddress{b}, *paid)
	})
}
