package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// seedInviter gives addr a member record with DREAM and invitation credits.
func seedInviter(t *testing.T, f *fixture, addr sdk.AccAddress) types.Member {
	t.Helper()
	seedMigrant(t, f, addr, 1_000_000_000)
	m := mustMember(t, f, addr)
	m.InvitationCredits = 2
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), m))
	return m
}

// assertInvitationVoided checks the invitation is REVOKED, the invitee slot is
// free and the inviter has its stake unlocked and its credit back.
func assertInvitationVoided(t *testing.T, f *fixture, invID uint64, inviter sdk.AccAddress, before types.Member) {
	t.Helper()
	inv, err := f.keeper.Invitation.Get(f.ctx, invID)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_REVOKED, inv.Status)
	has, err := f.keeper.InvitationsByInvitee.Has(f.ctx, inv.InviteeAddress)
	require.NoError(t, err)
	require.False(t, has, "invitee slot freed")
	after := mustMember(t, f, inviter)
	require.Equal(t, before.InvitationCredits, after.InvitationCredits, "credit restored")
	require.True(t, before.StakedDream.Equal(*after.StakedDream), "stake unlocked: before %s after %s", before.StakedDream, after.StakedDream)
	require.True(t, before.DreamBalance.Equal(*after.DreamBalance), "no DREAM lost")
}

// TestREPR02_RecoveryVoidsFrontRunInvitation: a thief sees the recovery
// target in the mempool and invites it before the reveal lands. The reveal
// still opens the RECOVERY migration and the invitation is voided with the
// inviter refunded, so the front-run burns nothing of the victim's.
func TestREPR02_RecoveryVoidsFrontRunInvitation(t *testing.T) {
	f := migrationFixture(t)
	victim := sdk.AccAddress([]byte("phoenix-victim_____"))
	target := sdk.AccAddress([]byte("phoenix-target_____"))
	thief := sdk.AccAddress([]byte("phoenix-thief______"))
	seedMigrant(t, f, victim, 1)
	before := seedInviter(t, f, thief)
	p := mustParams(t, f)

	preimage := []byte("aurora recovery code")
	require.NoError(t, f.keeper.SetRecoveryCommitment(f.ctx, victim.String(), types.RecoveryCommitmentHash(preimage), nil, true))
	require.NoError(t, f.keeper.CommitRecovery(f.ctx, target.String(), types.RecoverySeal(preimage, victim.String(), target.String())))
	advance(f, 0, p.RecoveryRevealMinBlocks)

	invID, err := f.keeper.CreateInvitation(f.ctx, thief, target, f.keeper.RequiredInvitationStakeForTest(f.ctx, thief), nil)
	require.NoError(t, err, "no migration request exists yet, so the invitation is accepted")

	id, reason, err := f.keeper.RevealRecovery(f.ctx, target.String(), victim.String(), target.String(), preimage)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotZero(t, id, "the RECOVERY migration opens")
	assertInvitationVoided(t, f, invID, thief, before)

	// The target is now reserved: the thief cannot re-invite it.
	_, err = f.keeper.CreateInvitation(f.ctx, thief, target, f.keeper.RequiredInvitationStakeForTest(f.ctx, thief), nil)
	require.ErrorIs(t, err, types.ErrMigrationTargetNotFresh)

	// The target can accept: nothing is left that fails freshness.
	require.NoError(t, f.keeper.AcceptMigration(f.ctx, target.String(), id))
}

// TestREPR02_CouncilForceVoidsPriorInvitation: the target of a force sits in
// a public commons proposal; an invitation filed before execution is voided
// at open instead of failing the force.
func TestREPR02_CouncilForceVoidsPriorInvitation(t *testing.T) {
	f := migrationFixture(t)
	victim := sdk.AccAddress([]byte("zenith-victim______"))
	target := sdk.AccAddress([]byte("zenith-target______"))
	thief := sdk.AccAddress([]byte("zenith-thief_______"))
	seedMigrant(t, f, victim, 1)
	before := seedInviter(t, f, thief)

	invID, err := f.keeper.CreateInvitation(f.ctx, thief, target, f.keeper.RequiredInvitationStakeForTest(f.ctx, thief), nil)
	require.NoError(t, err)

	id, err := f.keeper.ForceMigration(f.ctx, f.keeper.GetAuthorityString(), victim.String(), target.String(), "ipfs://evidence")
	require.NoError(t, err)
	require.NotZero(t, id)
	assertInvitationVoided(t, f, invID, thief, before)
	require.NoError(t, f.keeper.AcceptMigration(f.ctx, target.String(), id))
}

// TestREPR02_KeyPathKeepsInvitationRefusal: the KEY path can name any
// address, so a pending invitation still makes its target not fresh and is
// left untouched.
func TestREPR02_KeyPathKeepsInvitationRefusal(t *testing.T) {
	f := migrationFixture(t)
	member := sdk.AccAddress([]byte("aurora-member______"))
	target := sdk.AccAddress([]byte("aurora-target______"))
	inviter := sdk.AccAddress([]byte("aurora-inviter_____"))
	seedMigrant(t, f, member, 1)
	seedInviter(t, f, inviter)
	invID, err := f.keeper.CreateInvitation(f.ctx, inviter, target, f.keeper.RequiredInvitationStakeForTest(f.ctx, inviter), nil)
	require.NoError(t, err)

	_, err = f.keeper.RequestKeyMigration(f.ctx, member.String(), target.String())
	require.ErrorIs(t, err, types.ErrMigrationTargetNotFresh)
	inv, err := f.keeper.Invitation.Get(f.ctx, invID)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_PENDING, inv.Status)
}
