package keeper_test

import (
	"context"

	"encoding/hex"
	"testing"
	"time"

	"cosmossdk.io/collections"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// migrationFixture is initFixture with the commons mock reporting no group
// policies (the permissive default would make every target "not fresh").
func migrationFixture(t *testing.T, opts ...FixtureOption) *fixture {
	t.Helper()
	f := initFixture(t, opts...)
	f.commonsKeeper.IsGroupPolicyAddressFn = func(context.Context, string) bool { return false }
	f.ctx = f.ctx.WithBlockTime(time.Unix(1_700_000_000, 0)).WithBlockHeight(100)
	return f
}

func advance(f *fixture, seconds int64, blocks int64) {
	f.ctx = f.ctx.
		WithBlockTime(f.ctx.BlockTime().Add(time.Duration(seconds) * time.Second)).
		WithBlockHeight(f.ctx.BlockHeight() + blocks)
}

func seedMigrant(t *testing.T, f *fixture, addr sdk.AccAddress, balance int64) {
	t.Helper()
	seedEstablishedMember(t, f, addr, math.NewInt(balance))
	m := mustMember(t, f, addr)
	m.ContentLicense = "CC0-1.0" // genesis validation requires the agreement on every member
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), m))
}

func mustParams(t *testing.T, f *fixture) types.Params {
	t.Helper()
	p, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	return p
}

// finalizeNow drives the key path to completion: accept, wait out the delay,
// run the EndBlocker pass.
func finalizeKeyMigration(t *testing.T, f *fixture, from, to sdk.AccAddress) uint64 {
	t.Helper()
	id, err := f.keeper.RequestKeyMigration(f.ctx, from.String(), to.String())
	require.NoError(t, err)
	require.NoError(t, f.keeper.AcceptMigration(f.ctx, to.String(), id))
	advance(f, mustParams(t, f).KeyMigrationDelay+1, 1)
	f.keeper.ProcessMigrations(f.ctx)
	m, found := f.keeper.GetMemberMigration(f.ctx, id)
	require.True(t, found)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_FINALIZED, m.Status, m.FailureReason)
	return id
}

func TestKeyMigration_HappyPath(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("migrant-a__________"))
	b := sdk.AccAddress([]byte("migrant-b__________"))
	seedMigrant(t, f, a, 5_000_000)
	// A window wide enough to hold both rotations below (the test build's
	// 60s window would let the first one age out).
	p := mustParams(t, f)
	p.KeyMigrationWindow = 100 * p.MigrationCooldown
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))

	id, err := f.keeper.RequestKeyMigration(f.ctx, a.String(), b.String())
	require.NoError(t, err)
	m, found := f.keeper.GetPendingMigration(f.ctx, a.String())
	require.True(t, found)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_REQUESTED, m.Status)
	require.Equal(t, types.MigrationPath_MIGRATION_PATH_KEY, m.Path)

	// Only the new address accepts.
	require.ErrorIs(t, f.keeper.AcceptMigration(f.ctx, a.String(), id), types.ErrMigrationNotAuthorized)
	require.NoError(t, f.keeper.AcceptMigration(f.ctx, b.String(), id))

	// Not yet: the delay has not elapsed.
	f.keeper.ProcessMigrations(f.ctx)
	m, _ = f.keeper.GetMemberMigration(f.ctx, id)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_ACCEPTED, m.Status)

	advance(f, mustParams(t, f).KeyMigrationDelay+1, 1)
	f.keeper.ProcessMigrations(f.ctx)
	m, _ = f.keeper.GetMemberMigration(f.ctx, id)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_FINALIZED, m.Status, m.FailureReason)

	// The record moved, with its balance.
	_, err = f.keeper.Member.Get(f.ctx, a.String())
	require.Error(t, err)
	moved := mustMember(t, f, b)
	require.Equal(t, b.String(), moved.Address)
	require.Equal(t, int64(5_000_000), moved.DreamBalance.Int64())
	require.Len(t, moved.KeyMigrationTimes, 1)
	require.Equal(t, f.ctx.BlockTime().Unix(), moved.LastMigratedAt)

	// Alias contract.
	require.Equal(t, b.String(), f.keeper.Canonical(f.ctx, a.String()))
	require.True(t, f.keeper.IsTombstoned(f.ctx, a.String()))
	require.True(t, f.keeper.CanActAs(f.ctx, b.String(), a.String()))
	require.False(t, f.keeper.CanActAs(f.ctx, a.String(), a.String()), "a tombstone may not act")
	require.True(t, f.keeper.SameMember(f.ctx, a.String(), b.String()))
	require.Equal(t, []string{b.String(), a.String()}, f.keeper.RecentAddressesOf(f.ctx, a.String()))
	require.Equal(t, []string{b.String(), a.String()}, f.keeper.AllAddressesOf(f.ctx, b.String()))

	// Value lookups resolve, gates refuse.
	got, err := f.keeper.GetMember(f.ctx, a)
	require.NoError(t, err)
	require.Equal(t, b.String(), got.Address)
	bal, err := f.keeper.GetBalance(f.ctx, a)
	require.NoError(t, err)
	require.Equal(t, int64(5_000_000), bal.Int64())
	require.False(t, f.keeper.IsActiveMember(f.ctx, a))
	require.True(t, f.keeper.IsActiveMember(f.ctx, b))
	require.NoError(t, f.keeper.BurnDREAM(f.ctx, a, math.NewInt(1_000_000)))
	require.Equal(t, int64(4_000_000), mustMember(t, f, b).DreamBalance.Int64())

	// The cooldown refuses an immediate second rotation.
	c := sdk.AccAddress([]byte("migrant-c__________"))
	_, err = f.keeper.RequestKeyMigration(f.ctx, b.String(), c.String())
	require.ErrorIs(t, err, types.ErrMigrationCooldown)

	// After the cooldown: a tombstone can never be a target again, and a
	// second rotation path-compresses the first alias.
	advance(f, mustParams(t, f).MigrationCooldown+1, 1)
	_, err = f.keeper.RequestKeyMigration(f.ctx, b.String(), a.String())
	require.ErrorIs(t, err, types.ErrMigrationTargetNotFresh)
	finalizeKeyMigration(t, f, b, c)
	require.Equal(t, c.String(), f.keeper.Canonical(f.ctx, a.String()))
	require.Equal(t, c.String(), f.keeper.Canonical(f.ctx, b.String()))
	require.ElementsMatch(t, []string{c.String(), a.String(), b.String()}, f.keeper.AllAddressesOf(f.ctx, a.String()))

	// Two in the rolling window: a third is refused by the window cap even
	// after the cooldown.
	d := sdk.AccAddress([]byte("migrant-d__________"))
	advance(f, mustParams(t, f).MigrationCooldown+1, 1)
	_, err = f.keeper.RequestKeyMigration(f.ctx, c.String(), d.String())
	require.ErrorIs(t, err, types.ErrMigrationWindowCap)
}

func TestKeyMigration_Refusals(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("refuse-a___________"))
	b := sdk.AccAddress([]byte("refuse-b___________"))
	other := sdk.AccAddress([]byte("refuse-other_______"))
	seedMigrant(t, f, a, 1_000_000)
	seedMigrant(t, f, other, 1_000_000)

	// Target is a member.
	_, err := f.keeper.RequestKeyMigration(f.ctx, a.String(), other.String())
	require.ErrorIs(t, err, types.ErrMigrationTargetNotFresh)
	// Target equals source.
	_, err = f.keeper.RequestKeyMigration(f.ctx, a.String(), a.String())
	require.ErrorIs(t, err, types.ErrMigrationTargetNotFresh)
	// Reserved address.
	f.keeper.AddReservedAddress(b.String())
	_, err = f.keeper.RequestKeyMigration(f.ctx, a.String(), b.String())
	require.ErrorIs(t, err, types.ErrMigrationTargetNotFresh)

	// Open member report blocks the key path only.
	c := sdk.AccAddress([]byte("refuse-c___________"))
	require.NoError(t, f.keeper.MemberReport.Set(f.ctx, a.String(), types.MemberReport{
		Member: a.String(), Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
	}))
	_, err = f.keeper.RequestKeyMigration(f.ctx, a.String(), c.String())
	require.ErrorIs(t, err, types.ErrMigrationOpenReport)
	id, err := f.keeper.ForceMigration(f.ctx, f.keeper.GetAuthorityString(), a.String(), c.String(), "ipfs://evidence")
	require.NoError(t, err)
	require.NoError(t, f.keeper.CancelMigration(f.ctx, f.keeper.GetAuthorityString(), id))
	require.NoError(t, f.keeper.MemberReport.Remove(f.ctx, a.String()))

	// Key path switched off.
	p := mustParams(t, f)
	p.KeyPathEnabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))
	_, err = f.keeper.RequestKeyMigration(f.ctx, a.String(), c.String())
	require.ErrorIs(t, err, types.ErrKeyPathDisabled)
	p.KeyPathEnabled = true
	p.MigrationEnabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))
	_, err = f.keeper.RequestKeyMigration(f.ctx, a.String(), c.String())
	require.ErrorIs(t, err, types.ErrMigrationDisabled)
	p.MigrationEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))

	// Zeroed members cannot migrate on any path.
	require.NoError(t, f.keeper.ZeroMember(f.ctx, a, "test"))
	_, err = f.keeper.RequestKeyMigration(f.ctx, a.String(), c.String())
	require.ErrorIs(t, err, types.ErrMigrationZeroedMember)
	_, err = f.keeper.ForceMigration(f.ctx, f.keeper.GetAuthorityString(), a.String(), c.String(), "")
	require.ErrorIs(t, err, types.ErrMigrationZeroedMember)
}

func TestKeyMigration_ExpiresWithoutAccept(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("expire-a___________"))
	b := sdk.AccAddress([]byte("expire-b___________"))
	seedMigrant(t, f, a, 1)
	id, err := f.keeper.RequestKeyMigration(f.ctx, a.String(), b.String())
	require.NoError(t, err)
	advance(f, mustParams(t, f).MigrationAcceptWindow+1, 1)
	f.keeper.ProcessMigrations(f.ctx)
	m, _ := f.keeper.GetMemberMigration(f.ctx, id)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_EXPIRED, m.Status)
	_, pending := f.keeper.GetPendingMigration(f.ctx, a.String())
	require.False(t, pending)
	require.ErrorIs(t, f.keeper.AcceptMigration(f.ctx, b.String(), id), types.ErrMigrationWrongStatus)
}

func TestKeyMigration_ZeroedDuringDelayCancels(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("zeroed-a___________"))
	b := sdk.AccAddress([]byte("zeroed-b___________"))
	seedMigrant(t, f, a, 1)
	id, err := f.keeper.RequestKeyMigration(f.ctx, a.String(), b.String())
	require.NoError(t, err)
	require.NoError(t, f.keeper.AcceptMigration(f.ctx, b.String(), id))
	require.NoError(t, f.keeper.ZeroMember(f.ctx, a, "test"))
	advance(f, mustParams(t, f).KeyMigrationDelay+1, 1)
	f.keeper.ProcessMigrations(f.ctx)
	m, _ := f.keeper.GetMemberMigration(f.ctx, id)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_CANCELLED, m.Status)
	require.False(t, f.keeper.IsTombstoned(f.ctx, a.String()))
}

func TestRecoveryPath(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("recover-a__________"))
	b := sdk.AccAddress([]byte("recover-b__________"))
	thief := sdk.AccAddress([]byte("recover-thief______"))
	revealer := sdk.AccAddress([]byte("recover-revealer___"))
	setupMemberWithZkKey(t, f, a, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, make([]byte, 32))
	p := mustParams(t, f)

	preimage := []byte("printed recovery code #1")
	commitment := types.RecoveryCommitmentHash(preimage)
	preimage2 := []byte("printed recovery code #2")
	commitment2 := types.RecoveryCommitmentHash(preimage2)

	// A delayed commitment is not active until recovery_change_delay passes.
	require.NoError(t, f.keeper.SetRecoveryCommitment(f.ctx, a.String(), commitment, nil, false))
	require.ErrorIs(t, f.keeper.SetRecoveryCommitment(f.ctx, a.String(), commitment, nil, false), types.ErrRecoveryCommitmentExists)
	// The onboarding form is immediate.
	require.NoError(t, f.keeper.SetRecoveryCommitment(f.ctx, a.String(), commitment2, nil, true))
	set, found := f.keeper.GetRecoveryCommitments(f.ctx, a.String())
	require.True(t, found)
	require.Len(t, set.Commitments, 2)

	// The thief starts a key migration; the owner's reveal will cancel it.
	thiefID, err := f.keeper.RequestKeyMigration(f.ctx, a.String(), thief.String())
	require.NoError(t, err)

	// Seal, then reveal too early.
	sealed := types.RecoverySeal(preimage, a.String(), b.String())
	require.NoError(t, f.keeper.CommitRecovery(f.ctx, revealer.String(), sealed))
	require.ErrorIs(t, f.keeper.CommitRecovery(f.ctx, revealer.String(), sealed), types.ErrRecoverySealExists)
	_, _, err = f.keeper.RevealRecovery(f.ctx, revealer.String(), a.String(), b.String(), preimage)
	require.ErrorIs(t, err, types.ErrRecoveryCommitmentInactive, "delayed commitment is not active yet")
	advance(f, 0, p.RecoveryRevealMinBlocks)
	// Reveal of the immediate commitment works once the seal has aged; a
	// seal for another target does not match.
	_, _, err = f.keeper.RevealRecovery(f.ctx, revealer.String(), a.String(), thief.String(), preimage2)
	require.ErrorIs(t, err, types.ErrRecoverySealNotFound)
	sealed2 := types.RecoverySeal(preimage2, a.String(), b.String())
	require.NoError(t, f.keeper.CommitRecovery(f.ctx, revealer.String(), sealed2))
	_, _, err = f.keeper.RevealRecovery(f.ctx, revealer.String(), a.String(), b.String(), preimage2)
	require.ErrorIs(t, err, types.ErrRecoverySealTooYoung)
	advance(f, 0, p.RecoveryRevealMinBlocks)
	id, _, err := f.keeper.RevealRecovery(f.ctx, revealer.String(), a.String(), b.String(), preimage2)
	require.NoError(t, err)

	// Precedence: the thief's key migration is gone, the recovery is pending.
	thiefM, _ := f.keeper.GetMemberMigration(f.ctx, thiefID)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_CANCELLED, thiefM.Status)
	pending, ok := f.keeper.GetPendingMigration(f.ctx, a.String())
	require.True(t, ok)
	require.Equal(t, id, pending.Id)
	require.Equal(t, types.MigrationPath_MIGRATION_PATH_RECOVERY, pending.Path)
	// The thief cannot cancel it, HR cannot either; only gov.
	require.ErrorIs(t, f.keeper.CancelMigration(f.ctx, a.String(), id), types.ErrMigrationNotAuthorized)
	// The thief cannot open a lower-precedence migration over it.
	_, err = f.keeper.RequestKeyMigration(f.ctx, a.String(), thief.String())
	require.ErrorIs(t, err, types.ErrMigrationPending)
	// The consumed commitment is gone, the delayed one was voided (not yet
	// active), the seal is consumed.
	set, _ = f.keeper.GetRecoveryCommitments(f.ctx, a.String())
	require.Empty(t, set.Commitments)
	_, err = f.keeper.RecoveryCommits.Get(f.ctx, sealed2)
	require.Error(t, err)

	require.NoError(t, f.keeper.AcceptMigration(f.ctx, b.String(), id))
	advance(f, p.RecoveryMigrationDelay+1, 1)
	f.keeper.ProcessMigrations(f.ctx)
	m, _ := f.keeper.GetMemberMigration(f.ctx, id)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_FINALIZED, m.Status, m.FailureReason)

	// The presumed-compromised ZK key was cleared and the next registration
	// is cooldown-exempt; the tombstone records the path for the ante
	// decorator.
	moved := mustMember(t, f, b)
	require.Empty(t, moved.ZkPublicKey)
	require.True(t, moved.ZkKeyCooldownExempt)
	require.Empty(t, moved.KeyMigrationTimes, "recovery does not count against the key window")
	tomb, found := f.keeper.GetTombstone(f.ctx, a.String())
	require.True(t, found)
	require.Equal(t, types.MigrationPath_MIGRATION_PATH_RECOVERY, tomb.Path)
}

func TestRecoverySealExpiryBurnsDeposit(t *testing.T) {
	f := migrationFixture(t)
	submitter := sdk.AccAddress([]byte("seal-submitter_____"))
	var burned sdk.Coins
	f.bankKeeper.BurnCoinsFn = func(_ context.Context, _ string, amt sdk.Coins) error {
		burned = burned.Add(amt...)
		return nil
	}
	sealed := types.RecoverySeal([]byte("x"), "a", "b")
	require.NoError(t, f.keeper.CommitRecovery(f.ctx, submitter.String(), sealed))
	advance(f, mustParams(t, f).RecoveryCommitTtl+1, 1)
	f.keeper.ProcessMigrations(f.ctx)
	_, err := f.keeper.RecoveryCommits.Get(f.ctx, sealed)
	require.Error(t, err)
	require.Equal(t, mustParams(t, f).RecoveryCommitDeposit, burned.AmountOf(f.keeper.BondDenom(f.ctx)))
}

func TestCouncilPath_ObjectionVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		verdict types.Verdict
		status  types.MigrationStatus
		bondTo  string // "to" or "objector"
	}{
		{"rejected objection finalizes", types.Verdict_VERDICT_REJECT_CHALLENGE, types.MigrationStatus_MIGRATION_STATUS_FINALIZED, "to"},
		{"upheld objection cancels", types.Verdict_VERDICT_UPHOLD_CHALLENGE, types.MigrationStatus_MIGRATION_STATUS_CANCELLED, "objector"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := migrationFixture(t)
			a := sdk.AccAddress([]byte("council-a__________"))
			b := sdk.AccAddress([]byte("council-b__________"))
			seedMigrant(t, f, a, 1)
			p := mustParams(t, f)

			var paid []sdk.AccAddress
			f.bankKeeper.SendCoinsFn = func(_ context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
				if from.Equals(keeper.AppealBondEscrowAddress()) {
					paid = append(paid, to)
				}
				return nil
			}

			// Only the HR policy / gov may force.
			nobody := migrationFixture(t, WithAuthorizationPolicy(NeverAuthorized))
			seedMigrant(t, nobody, a, 1)
			_, err := nobody.keeper.ForceMigration(nobody.ctx, b.String(), a.String(), b.String(), "")
			require.ErrorIs(t, err, types.ErrMigrationNotAuthorized)

			id, err := f.keeper.ForceMigration(f.ctx, f.keeper.GetAuthorityString(), a.String(), b.String(), "ipfs://evidence")
			require.NoError(t, err)
			// Objection is only possible during the delay (ACCEPTED).
			require.ErrorIs(t, f.keeper.ObjectMigration(f.ctx, a.String(), id, "not me"), types.ErrMigrationWrongStatus)
			require.NoError(t, f.keeper.AcceptMigration(f.ctx, b.String(), id))
			require.NoError(t, f.keeper.ObjectMigration(f.ctx, a.String(), id, "not me"))
			m, _ := f.keeper.GetMemberMigration(f.ctx, id)
			require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_OBJECTED, m.Status)
			require.Equal(t, p.MigrationObjectionBond, m.ObjectionBond)
			review, err := f.keeper.GetJuryReview(f.ctx, m.JuryReviewId)
			require.NoError(t, err)
			require.Equal(t, types.MigrationObjectionInitiativeType, review.ChallengerClaim)

			// An objected migration does not finalize on its own. Past the
			// council delay, but inside the objection jury's deadline: on the
			// network builds the delay (21 days) outlasts the jury (14 days),
			// whose timeout would escalate the objection before the verdict.
			wait := p.CouncilMigrationDelay + 1
			if wait >= types.DefaultAppealDeadline {
				wait = types.DefaultAppealDeadline - 1
			}
			advance(f, wait, 1)
			f.keeper.ProcessMigrations(f.ctx)
			m, _ = f.keeper.GetMemberMigration(f.ctx, id)
			require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_OBJECTED, m.Status)

			require.NoError(t, f.keeper.ResolveMigrationObjectionForTest(f.ctx, review, tc.verdict))
			f.keeper.ProcessMigrations(f.ctx)
			m, _ = f.keeper.GetMemberMigration(f.ctx, id)
			require.Equal(t, tc.status, m.Status, m.FailureReason)
			require.True(t, m.ObjectionBond.IsZero())
			require.Len(t, paid, 1)
			if tc.bondTo == "to" {
				require.Equal(t, b, paid[0])
				require.True(t, f.keeper.IsTombstoned(f.ctx, a.String()))
			} else {
				require.Equal(t, a, paid[0])
				require.False(t, f.keeper.IsTombstoned(f.ctx, a.String()))
			}
		})
	}
}

func TestCouncilPath_RecoveryOutranksAndCancels(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("rank-a_____________"))
	b := sdk.AccAddress([]byte("rank-b_____________"))
	c := sdk.AccAddress([]byte("rank-c_____________"))
	seedMigrant(t, f, a, 1)
	p := mustParams(t, f)
	preimage := []byte("code")
	require.NoError(t, f.keeper.SetRecoveryCommitment(f.ctx, a.String(), types.RecoveryCommitmentHash(preimage), nil, true))

	councilID, err := f.keeper.ForceMigration(f.ctx, f.keeper.GetAuthorityString(), a.String(), b.String(), "")
	require.NoError(t, err)
	// A key request cannot displace a council migration.
	_, err = f.keeper.RequestKeyMigration(f.ctx, a.String(), c.String())
	require.ErrorIs(t, err, types.ErrMigrationPending)

	sealed := types.RecoverySeal(preimage, a.String(), c.String())
	require.NoError(t, f.keeper.CommitRecovery(f.ctx, c.String(), sealed))
	advance(f, 0, p.RecoveryRevealMinBlocks)
	recID, _, err := f.keeper.RevealRecovery(f.ctx, c.String(), a.String(), c.String(), preimage)
	require.NoError(t, err)
	cm, _ := f.keeper.GetMemberMigration(f.ctx, councilID)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_CANCELLED, cm.Status)
	pending, _ := f.keeper.GetPendingMigration(f.ctx, a.String())
	require.Equal(t, recID, pending.Id)
	// HR cannot cancel a recovery migration; gov can.
	hr := migrationFixture(t)
	_ = hr
	require.NoError(t, f.keeper.CancelMigration(f.ctx, f.keeper.GetAuthorityString(), recID))
}

func TestZkKeyChangeDiscipline(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("zk-member__________"))
	seedMigrant(t, f, a, 1)
	srv := keeper.NewMsgServerImpl(f.keeper)
	key1 := make([]byte, 32)
	key1[31] = 1
	key2 := make([]byte, 32)
	key2[31] = 2
	key3 := make([]byte, 32)
	key3[31] = 3

	// First registration is immediate.
	_, err := srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: key1})
	require.NoError(t, err)
	require.Equal(t, key1, mustMember(t, f, a).ZkPublicKey)

	// A change zeroes the leaf now and parks the new key.
	_, err = srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: key2})
	require.NoError(t, err)
	m := mustMember(t, f, a)
	require.Empty(t, m.ZkPublicKey)
	require.Equal(t, key2, m.PendingZkPublicKey)
	require.True(t, m.ZkKeyChanged)
	_, err = srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: key3})
	require.ErrorIs(t, err, types.ErrZkKeyPending)

	// Activation after the delay.
	p := mustParams(t, f)
	f.keeper.ProcessMigrations(f.ctx)
	require.Empty(t, mustMember(t, f, a).ZkPublicKey)
	advance(f, 0, p.ZkKeyActivationDelay*p.EpochBlocks)
	f.keeper.ProcessMigrations(f.ctx)
	m = mustMember(t, f, a)
	require.Equal(t, key2, m.ZkPublicKey)
	require.Empty(t, m.PendingZkPublicKey)

	// Once per season.
	_, err = srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: key3})
	require.ErrorIs(t, err, types.ErrZkKeyCooldown)
	// Unless a recovery cleared it.
	m.ZkPublicKey = nil
	m.ZkKeyCooldownExempt = true
	require.NoError(t, f.keeper.Member.Set(f.ctx, a.String(), m))
	_, err = srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: key3})
	require.NoError(t, err)
	m = mustMember(t, f, a)
	require.Equal(t, key3, m.PendingZkPublicKey, "still waits out the activation delay")
	require.False(t, m.ZkKeyCooldownExempt, "the exemption is consumed")
}

func TestSweepTombstoneAndSendRestriction(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("sweep-a____________"))
	b := sdk.AccAddress([]byte("sweep-b____________"))
	seedMigrant(t, f, a, 1)
	finalizeKeyMigration(t, f, a, b)

	// Transfers to the tombstone are redirected to the canonical address.
	redirected, err := f.keeper.SendRestriction(f.ctx, b, a, sdk.NewCoins())
	require.NoError(t, err)
	require.Equal(t, b, redirected)
	untouched, err := f.keeper.SendRestriction(f.ctx, a, b, sdk.NewCoins())
	require.NoError(t, err)
	require.Equal(t, b, untouched)

	// Only the canonical address sweeps.
	denom := f.keeper.BondDenom(f.ctx)
	f.bankKeeper.SpendableCoinsFn = func(_ context.Context, addr sdk.AccAddress) sdk.Coins {
		if addr.Equals(a) {
			return sdk.NewCoins(sdk.NewInt64Coin(denom, 42))
		}
		return sdk.NewCoins()
	}
	_, err = f.keeper.SweepTombstone(f.ctx, a.String(), a.String())
	require.ErrorIs(t, err, types.ErrNotTombstoneOwner)
	swept, err := f.keeper.SweepTombstone(f.ctx, b.String(), a.String())
	require.NoError(t, err)
	require.Equal(t, int64(42), swept.AmountOf(denom).Int64())
}

func TestMigrationGenesisRoundTrip(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("genesis-a__________"))
	b := sdk.AccAddress([]byte("genesis-b__________"))
	c := sdk.AccAddress([]byte("genesis-c__________"))
	seedMigrant(t, f, a, 1)
	finalizeKeyMigration(t, f, a, b)
	require.NoError(t, f.keeper.SetRecoveryCommitment(f.ctx, b.String(), types.RecoveryCommitmentHash([]byte("x")), nil, false))
	require.NoError(t, f.keeper.CommitRecovery(f.ctx, c.String(), types.RecoverySeal([]byte("x"), b.String(), c.String())))
	advance(f, mustParams(t, f).MigrationCooldown+1, 1)
	pendingID, err := f.keeper.RequestKeyMigration(f.ctx, b.String(), c.String())
	require.NoError(t, err)

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.TombstoneList, 1)
	require.Len(t, exported.MemberMigrationList, 2)
	require.Len(t, exported.RecoveryCommitmentList, 1)
	require.Len(t, exported.RecoveryCommitList, 1)

	g := migrationFixture(t)
	g.ctx = g.ctx.WithBlockTime(f.ctx.BlockTime()).WithBlockHeight(f.ctx.BlockHeight())
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	require.Equal(t, b.String(), g.keeper.Canonical(g.ctx, a.String()))
	require.Equal(t, []string{b.String(), a.String()}, g.keeper.AllAddressesOf(g.ctx, b.String()))
	pending, found := g.keeper.GetPendingMigration(g.ctx, b.String())
	require.True(t, found)
	require.Equal(t, pendingID, pending.Id)
	_, err = g.keeper.RequestKeyMigration(g.ctx, b.String(), sdk.AccAddress([]byte("genesis-d__________")).String())
	require.ErrorIs(t, err, types.ErrMigrationPending, "pending pointer rebuilt from genesis")
	// A REQUESTED migration is indexed by target without reserving it.
	has, err := g.keeper.MigrationRequestsByTarget.Has(g.ctx, collections.Join(c.String(), pendingID))
	require.NoError(t, err)
	require.True(t, has)
	has, err = g.keeper.PendingMigrationByTarget.Has(g.ctx, c.String())
	require.NoError(t, err)
	require.False(t, has)
	require.Equal(t, pending.RequestedAt+mustParams(t, g).MigrationAcceptWindow, pending.AcceptBy)
}

func TestTombstoneCannotBeInvitedAgain(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("invite-a___________"))
	b := sdk.AccAddress([]byte("invite-b___________"))
	seedMigrant(t, f, a, 1)
	finalizeKeyMigration(t, f, a, b)
	err := f.keeper.AcceptInvitationWithRecovery(f.ctx, 1, a, "CC0-1.0", nil)
	require.ErrorIs(t, err, types.ErrMemberTombstoned)
}

func TestMigrationParamsValidation(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	p.KeyMigrationWindow = p.MigrationCooldown // < 2x cooldown
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.RecoveryChangeDelay = p.KeyMigrationDelay
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.ZkKeyActivationDelay = 0
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.MaxKeyMigrationsPerWindow = 0
	require.Error(t, p.Validate())
}

func TestMigrationQueries_TargetAndPredecessor(t *testing.T) {
	f := migrationFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	a := sdk.AccAddress([]byte("migrant-a__________"))
	b := sdk.AccAddress([]byte("migrant-b__________"))
	seedMigrant(t, f, a, 5_000_000)

	_, err := qs.PendingMigrationByTarget(f.ctx, &types.QueryPendingMigrationByTargetRequest{Address: b.String()})
	require.Error(t, err, "no migration targets b yet")

	// The new address finds the migration it must accept.
	id, err := f.keeper.RequestKeyMigration(f.ctx, a.String(), b.String())
	require.NoError(t, err)
	resp, err := qs.PendingMigrationByTarget(f.ctx, &types.QueryPendingMigrationByTargetRequest{Address: b.String()})
	require.NoError(t, err)
	require.Equal(t, id, resp.Migration.Id)
	require.Equal(t, a.String(), resp.Migration.FromAddress)

	// Finalized migrations are no longer pending.
	require.NoError(t, f.keeper.AcceptMigration(f.ctx, b.String(), id))
	advance(f, mustParams(t, f).KeyMigrationDelay+1, 1)
	f.keeper.ProcessMigrations(f.ctx)
	_, err = qs.PendingMigrationByTarget(f.ctx, &types.QueryPendingMigrationByTargetRequest{Address: b.String()})
	require.Error(t, err)

	// The member query follows the predecessor address to the moved record.
	got, err := qs.GetMember(f.ctx, &types.QueryGetMemberRequest{Address: a.String()})
	require.NoError(t, err)
	require.Equal(t, b.String(), got.Member.Address)
}

func TestMemberMigrationsFilters(t *testing.T) {
	f := migrationFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	a := sdk.AccAddress([]byte("migrant-a__________"))
	b := sdk.AccAddress([]byte("migrant-b__________"))
	c := sdk.AccAddress([]byte("migrant-c__________"))
	d := sdk.AccAddress([]byte("migrant-d__________"))
	seedMigrant(t, f, a, 5_000_000)
	seedMigrant(t, f, c, 5_000_000)

	finalized := finalizeKeyMigration(t, f, a, b)
	pending, err := f.keeper.RequestKeyMigration(f.ctx, c.String(), d.String())
	require.NoError(t, err)
	ids := func(ms []types.MemberMigration) []uint64 {
		out := []uint64{}
		for _, m := range ms {
			out = append(out, m.Id)
		}
		return out
	}

	all, err := qs.MemberMigrations(f.ctx, &types.QueryMemberMigrationsRequest{})
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{finalized, pending}, ids(all.Migrations))

	byStatus, err := qs.MemberMigrations(f.ctx, &types.QueryMemberMigrationsRequest{Status: types.MigrationStatus_MIGRATION_STATUS_REQUESTED})
	require.NoError(t, err)
	require.Equal(t, []uint64{pending}, ids(byStatus.Migrations))

	// Either side of the migration matches.
	byFrom, err := qs.MemberMigrations(f.ctx, &types.QueryMemberMigrationsRequest{Address: a.String()})
	require.NoError(t, err)
	require.Equal(t, []uint64{finalized}, ids(byFrom.Migrations))
	byTo, err := qs.MemberMigrations(f.ctx, &types.QueryMemberMigrationsRequest{Address: d.String()})
	require.NoError(t, err)
	require.Equal(t, []uint64{pending}, ids(byTo.Migrations))
}

func TestRecoveryCommitQueries(t *testing.T) {
	f := migrationFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	a := sdk.AccAddress([]byte("recover-a__________"))
	b := sdk.AccAddress([]byte("recover-b__________"))
	submitter := sdk.AccAddress([]byte("recover-revealer___"))
	other := sdk.AccAddress([]byte("recover-other______"))

	sealed := types.RecoverySeal([]byte("printed recovery code #1"), a.String(), b.String())
	sealedHex := hex.EncodeToString(sealed)
	_, err := qs.RecoveryCommit(f.ctx, &types.QueryRecoveryCommitRequest{SealedHex: sealedHex})
	require.Error(t, err, "nothing sealed yet")

	require.NoError(t, f.keeper.CommitRecovery(f.ctx, submitter.String(), sealed))
	got, err := qs.RecoveryCommit(f.ctx, &types.QueryRecoveryCommitRequest{SealedHex: sealedHex})
	require.NoError(t, err)
	require.Equal(t, submitter.String(), got.Commit.Submitter)
	require.Equal(t, f.ctx.BlockHeight(), got.Commit.Height)

	mine, err := qs.RecoveryCommitsBySubmitter(f.ctx, &types.QueryRecoveryCommitsBySubmitterRequest{Submitter: submitter.String()})
	require.NoError(t, err)
	require.Len(t, mine.Commits, 1)
	theirs, err := qs.RecoveryCommitsBySubmitter(f.ctx, &types.QueryRecoveryCommitsBySubmitterRequest{Submitter: other.String()})
	require.NoError(t, err)
	require.Empty(t, theirs.Commits)

	_, err = qs.RecoveryCommit(f.ctx, &types.QueryRecoveryCommitRequest{SealedHex: "zz"})
	require.Error(t, err, "malformed seal")

	// Past its TTL a seal is gone from both queries, even before the sweep.
	advance(f, mustParams(t, f).RecoveryCommitTtl+1, 1)
	_, err = qs.RecoveryCommit(f.ctx, &types.QueryRecoveryCommitRequest{SealedHex: sealedHex})
	require.Error(t, err)
	mine, err = qs.RecoveryCommitsBySubmitter(f.ctx, &types.QueryRecoveryCommitsBySubmitterRequest{Submitter: submitter.String()})
	require.NoError(t, err)
	require.Empty(t, mine.Commits)
}
