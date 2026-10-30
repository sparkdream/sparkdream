package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Regression tests for the member-key-migration findings of the
// 2026-10-26 security review (REP-R-xx).

// TestREPR01_RolePrimitivesFollowTheAlias: a sentinel reserves bond for a
// hide, migrates, and the appeal is overturned against the stored (old)
// address. The slash, the release, the overturn streak and the cooldown must
// all land on the re-keyed record, and the tombstone must not pass the
// action-time gate.
func TestREPR01_RolePrimitivesFollowTheAlias(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("role-alias-a_______"))
	b := sdk.AccAddress([]byte("role-alias-b_______"))
	seedMigrant(t, f, a, 10_000_000)
	rt := types.RoleType_ROLE_TYPE_CONTENT_SENTINEL
	seedBondedRoleConfig(t, f, rt, 1_000_000, 500_000)

	// Bond 5 DREAM: locked on the member, recorded on the role.
	m := mustMember(t, f, a)
	staked := math.NewInt(5_000_000)
	m.StakedDream = &staked
	require.NoError(t, f.keeper.Member.Set(f.ctx, a.String(), m))
	require.NoError(t, f.keeper.BondedRoles.Set(f.ctx, collections.Join(int32(rt), a.String()), types.BondedRole{
		Address: a.String(), RoleType: rt, CurrentBond: "5000000", TotalCommittedBond: "0",
		BondStatus: types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
	}))
	// Two hides each reserve 1 DREAM.
	require.NoError(t, f.keeper.ReserveBond(f.ctx, rt, a.String(), math.NewInt(1_000_000)))
	require.NoError(t, f.keeper.ReserveBond(f.ctx, rt, a.String(), math.NewInt(1_000_000)))

	finalizeKeyMigration(t, f, a, b)

	// The record moved; lookups by the old address find it.
	br, err := f.keeper.GetBondedRole(f.ctx, rt, a.String())
	require.NoError(t, err)
	require.Equal(t, b.String(), br.Address)
	ok, err := f.keeper.IsBondedRole(f.ctx, rt, a.String())
	require.NoError(t, err)
	require.True(t, ok)

	// The tombstone may not act on the role.
	_, err = f.keeper.EligibleForRole(f.ctx, rt, a.String())
	require.ErrorIs(t, err, types.ErrMemberTombstoned)
	_, err = f.keeper.EligibleForRole(f.ctx, rt, b.String())
	require.NoError(t, err)

	// Overturn of hide #1: slash by the stored (old) address.
	require.NoError(t, f.keeper.SlashBond(f.ctx, rt, a.String(), math.NewInt(1_000_000), "appeal_overturned"))
	require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx, rt, a.String(), types.ActionKindForumHide, false))
	// Upheld hide #2: release by the stored address.
	require.NoError(t, f.keeper.ReleaseBond(f.ctx, rt, a.String(), math.NewInt(1_000_000)))

	br, err = f.keeper.GetBondedRole(f.ctx, rt, b.String())
	require.NoError(t, err)
	require.Equal(t, "4000000", br.CurrentBond, "the slash reached the migrated bond")
	require.Equal(t, "0", br.TotalCommittedBond, "both reservations were cleared")
	require.Equal(t, int64(4_000_000), mustMember(t, f, b).StakedDream.Int64())

	// The verdict landed on the migrated activity record, no ghost under A.
	ra, err := f.keeper.RoleActivities.Get(f.ctx, collections.Join(int32(rt), b.String()))
	require.NoError(t, err)
	require.Equal(t, uint64(1), ra.ConsecutiveOverturns)
	require.Greater(t, ra.OverturnCooldownUntil, int64(0))
	has, err := f.keeper.RoleActivities.Has(f.ctx, collections.Join(int32(rt), a.String()))
	require.NoError(t, err)
	require.False(t, has, "no ghost RoleActivity under the tombstone")
	require.Equal(t, ra.OverturnCooldownUntil, f.keeper.RoleOverturnCooldownUntil(f.ctx, rt, a.String()))
}

// TestREPR02_RequestDoesNotSquatTarget: a KEY request (by anyone, the thief
// included) naming the victim's recovery target no longer reserves it, so
// the victim's reveal and accept go through; the losing request then fails
// at its own accept.
func TestREPR02_RequestDoesNotSquatTarget(t *testing.T) {
	f := migrationFixture(t)
	victim := sdk.AccAddress([]byte("squat-victim_______"))
	squatter := sdk.AccAddress([]byte("squat-squatter_____"))
	target := sdk.AccAddress([]byte("squat-target_______"))
	seedMigrant(t, f, victim, 1)
	seedMigrant(t, f, squatter, 1)
	p := mustParams(t, f)
	preimage := []byte("victim recovery code")
	require.NoError(t, f.keeper.SetRecoveryCommitment(f.ctx, victim.String(), types.RecoveryCommitmentHash(preimage), nil, true))

	squatID, err := f.keeper.RequestKeyMigration(f.ctx, squatter.String(), target.String())
	require.NoError(t, err)
	require.False(t, f.keeper.IsReservedMigrationTarget(f.ctx, target.String()), "a KEY request reserves nothing")

	require.NoError(t, f.keeper.CommitRecovery(f.ctx, target.String(), types.RecoverySeal(preimage, victim.String(), target.String())))
	advance(f, 0, p.RecoveryRevealMinBlocks)
	id, reason, err := f.keeper.RevealRecovery(f.ctx, target.String(), victim.String(), target.String(), preimage)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotZero(t, id)
	// A pending RECOVERY request reserves the target against invitations.
	require.True(t, f.keeper.IsReservedMigrationTarget(f.ctx, target.String()))

	require.NoError(t, f.keeper.AcceptMigration(f.ctx, target.String(), id))
	// The squatter's request cannot be accepted any more.
	require.ErrorIs(t, f.keeper.AcceptMigration(f.ctx, target.String(), squatID), types.ErrMigrationTargetNotFresh)
	advance(f, p.RecoveryMigrationDelay+1, 1)
	f.keeper.ProcessMigrations(f.ctx)
	m, _ := f.keeper.GetMemberMigration(f.ctx, id)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_FINALIZED, m.Status, m.FailureReason)
}

// TestREPR02_InvitationToRecoveryTargetRefused: a thief cannot invite the
// recovery target to make it "not fresh".
func TestREPR02_InvitationToRecoveryTargetRefused(t *testing.T) {
	f := migrationFixture(t)
	victim := sdk.AccAddress([]byte("inv-victim_________"))
	thief := sdk.AccAddress([]byte("inv-thief__________"))
	target := sdk.AccAddress([]byte("inv-target_________"))
	seedMigrant(t, f, victim, 1)
	seedMigrant(t, f, thief, 1_000_000_000)
	tm := mustMember(t, f, thief)
	tm.InvitationCredits = 3
	require.NoError(t, f.keeper.Member.Set(f.ctx, thief.String(), tm))
	_, err := f.keeper.ForceMigration(f.ctx, f.keeper.GetAuthorityString(), victim.String(), target.String(), "")
	require.NoError(t, err)
	_, err = f.keeper.CreateInvitation(f.ctx, thief, target, math.NewInt(1_000_000_000), nil)
	require.ErrorIs(t, err, types.ErrMigrationTargetNotFresh)
}

// TestREPR03_FailedRevealConsumesCommitment: once the preimage and the aged
// seal check out, a reveal whose migration cannot open still consumes the
// commitment, so the leaked preimage cannot be re-sealed by whoever induced
// the failure.
func TestREPR03_FailedRevealConsumesCommitment(t *testing.T) {
	f := migrationFixture(t)
	victim := sdk.AccAddress([]byte("leak-victim________"))
	target := sdk.AccAddress([]byte("leak-target________"))
	thief2 := sdk.AccAddress([]byte("leak-thief2________"))
	seedMigrant(t, f, victim, 1)
	p := mustParams(t, f)
	preimage := []byte("leaked recovery code")
	require.NoError(t, f.keeper.SetRecoveryCommitment(f.ctx, victim.String(), types.RecoveryCommitmentHash(preimage), nil, true))
	sealed := types.RecoverySeal(preimage, victim.String(), target.String())
	require.NoError(t, f.keeper.CommitRecovery(f.ctx, target.String(), sealed))
	advance(f, 0, p.RecoveryRevealMinBlocks)

	// Induced failure: the target became a member before the reveal landed.
	seedMigrant(t, f, target, 1)
	id, reason, err := f.keeper.RevealRecovery(f.ctx, target.String(), victim.String(), target.String(), preimage)
	require.NoError(t, err, "a valid reveal succeeds even when the migration cannot open")
	require.Zero(t, id)
	require.Contains(t, reason, "not fresh")
	_, pending := f.keeper.GetPendingMigration(f.ctx, victim.String())
	require.False(t, pending)
	set, _ := f.keeper.GetRecoveryCommitments(f.ctx, victim.String())
	require.Empty(t, set.Commitments, "the commitment is consumed")
	_, err = f.keeper.RecoveryCommits.Get(f.ctx, sealed)
	require.Error(t, err, "the seal is consumed")

	// The attacker re-seals the now public preimage to their own address.
	require.NoError(t, f.keeper.CommitRecovery(f.ctx, thief2.String(), types.RecoverySeal(preimage, victim.String(), thief2.String())))
	advance(f, 0, p.RecoveryRevealMinBlocks)
	_, _, err = f.keeper.RevealRecovery(f.ctx, thief2.String(), victim.String(), thief2.String(), preimage)
	require.ErrorIs(t, err, types.ErrRecoveryCommitmentInactive)
}

// TestREPR09_MemberReportFollowsTheIdentity: a report names the member's
// canonical address, so a report against a tombstone lands on the successor,
// trips HasOpenMemberReport for it, and the successor can defend it. Reports
// against non-members are refused.
func TestREPR09_MemberReportFollowsTheIdentity(t *testing.T) {
	f := migrationFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	a := sdk.AccAddress([]byte("report-alias-a_____"))
	b := sdk.AccAddress([]byte("report-alias-b_____"))
	reporter := sdk.AccAddress([]byte("report-alias-rep___"))
	seedMigrant(t, f, a, 1)
	setSentinelReporter(t, f.keeper, f.ctx, reporter)
	finalizeKeyMigration(t, f, a, b)

	_, err := ms.ReportMember(f.ctx, &types.MsgReportMember{Creator: reporter.String(), Member: a.String(), Reason: "spam"})
	require.NoError(t, err)
	_, err = f.keeper.MemberReport.Get(f.ctx, a.String())
	require.Error(t, err, "nothing keyed by the tombstone")
	report, err := f.keeper.MemberReport.Get(f.ctx, b.String())
	require.NoError(t, err)
	require.Equal(t, b.String(), report.Member)
	require.True(t, f.keeper.HasOpenMemberReport(f.ctx, b.String()))

	// A second report through either address is the same open report.
	_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{Creator: reporter.String(), Member: b.String(), Reason: "again"})
	require.ErrorIs(t, err, types.ErrReportAlreadyExists)

	// The successor defends; the tombstone cannot.
	_, err = ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{Creator: a.String(), Defense: "x"})
	require.ErrorIs(t, err, types.ErrMemberTombstoned)
	_, err = ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{Creator: b.String(), Defense: "not me"})
	require.NoError(t, err)

	nobody := sdk.AccAddress([]byte("report-alias-nobody"))
	_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{Creator: reporter.String(), Member: nobody.String(), Reason: "x"})
	require.ErrorIs(t, err, types.ErrMemberNotFound)
}

// TestREPR11_GiftFollowsMigratedInviter: a migrated inviter can still gift
// its invitee, and the lifetime cap counts what it gave under the old
// address too.
func TestREPR11_GiftFollowsMigratedInviter(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("gift-inviter-a_____"))
	b := sdk.AccAddress([]byte("gift-inviter-b_____"))
	x := sdk.AccAddress([]byte("gift-invitee-x_____"))
	seedMigrant(t, f, a, 2_000_000_000)
	seedMigrant(t, f, x, 1)
	xm := mustMember(t, f, x)
	xm.InvitedBy = a.String()
	require.NoError(t, f.keeper.Member.Set(f.ctx, x.String(), xm))
	p := mustParams(t, f)
	p.GiftOnlyToInvitees = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))
	cap := p.MaxGiftPerInvitee
	gift := types.TransferPurpose_TRANSFER_PURPOSE_GIFT

	first := cap.QuoRaw(2)
	require.NoError(t, f.keeper.TransferDREAM(f.ctx, a, x, first, gift))
	finalizeKeyMigration(t, f, a, b)

	// The successor is still the inviter...
	require.NoError(t, f.keeper.TransferDREAM(f.ctx, b, x, cap.Sub(first), gift))
	// ...and the cap spans both addresses.
	err := f.keeper.TransferDREAM(f.ctx, b, x, math.NewInt(1), gift)
	require.ErrorIs(t, err, types.ErrExceedsGiftAllowance)
}

// TestREPR13_AcceptDeadlineIsStored: a change of migration_accept_window
// after the request neither orphans the expiry entry nor moves the deadline.
func TestREPR13_AcceptDeadlineIsStored(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("acceptby-a_________"))
	b := sdk.AccAddress([]byte("acceptby-b_________"))
	seedMigrant(t, f, a, 1)
	p := mustParams(t, f)
	id, err := f.keeper.RequestKeyMigration(f.ctx, a.String(), b.String())
	require.NoError(t, err)
	m, _ := f.keeper.GetMemberMigration(f.ctx, id)
	require.Equal(t, m.RequestedAt+p.MigrationAcceptWindow, m.AcceptBy)

	p.MigrationAcceptWindow *= 3
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))
	require.NoError(t, f.keeper.AcceptMigration(f.ctx, b.String(), id))
	has, err := f.keeper.MigrationAcceptExpiry.Has(f.ctx, collections.Join(m.AcceptBy, id))
	require.NoError(t, err)
	require.False(t, has, "the stored deadline's entry was removed on accept")
}

// TestREPR10_IndexesSurviveGenesis: the appeal, unbond and objection indexes
// that replaced full-store scans are rebuilt on import and drive the
// EndBlock passes.
func TestREPR10_IndexesSurviveGenesis(t *testing.T) {
	f := migrationFixture(t)
	appellant := sdk.AccAddress([]byte("idx-appellant______"))
	seedMigrant(t, f, appellant, 1)
	appealID, initiativeID, err := f.keeper.CreateGovActionAppeal(f.ctx, types.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK, "77", appellant, "x")
	require.NoError(t, err)

	rt := types.RoleType_ROLE_TYPE_CONTENT_SENTINEL
	holder := sdk.AccAddress([]byte("idx-holder_________"))
	require.NoError(t, f.keeper.SetBondedRole(f.ctx, types.BondedRole{
		Address: holder.String(), RoleType: rt, BondStatus: types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING,
		CurrentBond: "0", TotalCommittedBond: "0", PendingUnbondAmount: "0",
		UnbondCompletionTime: f.ctx.BlockTime().Unix() + 10,
	}))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	g := migrationFixture(t)
	g.ctx = g.ctx.WithBlockTime(f.ctx.BlockTime()).WithBlockHeight(f.ctx.BlockHeight())
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))

	got, err := g.keeper.GovActionAppealByInitiative.Get(g.ctx, initiativeID)
	require.NoError(t, err)
	require.Equal(t, appealID, got)
	a, _ := g.keeper.GovActionAppeal.Get(g.ctx, appealID)
	has, err := g.keeper.PendingGovActionAppeals.Has(g.ctx, collections.Join(a.Deadline, appealID))
	require.NoError(t, err)
	require.True(t, has)
	has, err = g.keeper.BondedRoleUnbondQueue.Has(g.ctx, collections.Join3(f.ctx.BlockTime().Unix()+10, int32(rt), holder.String()))
	require.NoError(t, err)
	require.True(t, has)

	// The timeout pass finds the appeal through the queue and drains it.
	late := g.ctx.WithBlockTime(g.ctx.BlockTime().Add(time.Duration(types.DefaultAppealDeadline+10) * time.Second))
	require.NoError(t, g.keeper.TimeoutExpiredAppeals(late))
	a, _ = g.keeper.GovActionAppeal.Get(late, appealID)
	require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT, a.Status)
	has, err = g.keeper.PendingGovActionAppeals.Has(late, collections.Join(a.Deadline, appealID))
	require.NoError(t, err)
	require.False(t, has)

	// The unbond matures from the queue and leaves it.
	require.NoError(t, g.keeper.MatureUnbonds(late))
	br, err := g.keeper.GetBondedRole(late, rt, holder.String())
	require.NoError(t, err)
	require.NotEqual(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING, br.BondStatus)
}
