package keeper_test

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	query "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// The P0.1 acceptance suite (Mastodon live link, see test/federation/mastodon/README.md): a verifier's
// committed bond must return to available on every DISPUTED/CHALLENGED
// exit except an upheld verdict, where SlashBond already frees it.
//
// All verdict paths here run through the real EndBlocker queues, not by
// calling the apply* helpers directly, so the tests also pin the queue
// interactions (Phase 6/7/8 ordering, the EscalatedChallenges guard).

// seededVerifierBond is what bondTestVerifier seeds as CurrentBond —
// above every network's MinVerifierBond so the tests can observe slashes
// and re-verifications without hitting the minimum.
var seededVerifierBond = math.NewInt(500_000_000)

// endBlockerAt runs the federation EndBlocker with the fixture's block
// time advanced by d, returning the context it ran under.
func endBlockerAt(t *testing.T, f *fixture, d time.Duration) {
	t.Helper()
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	require.NoError(t, f.keeper.EndBlocker(sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(d))))
}

func committedBond(t *testing.T, f *fixture, verifier string) math.Int {
	t.Helper()
	br, err := f.repKeeper.GetBondedRole(f.ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifier)
	require.NoError(t, err)
	committed, ok := math.NewIntFromString(br.TotalCommittedBond)
	require.True(t, ok)
	return committed
}

func currentBond(t *testing.T, f *fixture, verifier string) math.Int {
	t.Helper()
	br, err := f.repKeeper.GetBondedRole(f.ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifier)
	require.NoError(t, err)
	current, ok := math.NewIntFromString(br.CurrentBond)
	require.True(t, ok)
	return current
}

// challengeQuorum drives content to a quorum verdict: verify (hash
// match), challenge, then three arbiters submitting quorumHash. Returns
// the verifier address and the challenger address.
func challengeQuorum(t *testing.T, f *fixture, ms types.MsgServer, peer, seed string, quorumHash []byte) (verifier, challenger string, contentID uint64) {
	t.Helper()
	opStr := registerTestBridge(t, f, ms, peer, seed+"-op")
	hash := sha256.Sum256([]byte("quorum content " + seed))
	contentID = submitTestContent(t, f, ms, opStr, peer, hash[:])

	verifier = bondTestVerifier(t, f, ms, seed+"-verif")
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifier, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)

	challenger = testAddr(t, f, seed+"-challenger")
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: challenger, ContentId: contentID,
		ContentHash: quorumHash, Evidence: "content was modified",
	})
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		arbiter := registerTestBridge(t, f, ms, peer, seed+"-arbiter-"+strconv.Itoa(i))
		_, err = ms.SubmitArbiterHash(f.ctx, &types.MsgSubmitArbiterHash{
			Creator: arbiter, ContentId: contentID, ContentHash: quorumHash,
		})
		require.NoError(t, err)
	}
	return verifier, challenger, contentID
}

// Row 1 of the leak table: arbiter quorum says the verifier was RIGHT —
// applyAutoVerdictRejected must release the committed bond.
func TestVerdictRejectedReleasesCommitment(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "vr-peer")

	// Quorum hash == verifier hash → VERIFIER_RIGHT. The quorum hash
	// must equal both the verifier's hash and the content hash for the
	// match path to have stashed a VERIFIER_RIGHT verdict.
	verifierHash := sha256.Sum256([]byte("quorum content vr-x"))
	verifier, _, contentID := challengeQuorum(t, f, ms, "vr-peer", "vr-x", verifierHash[:])

	slash := types.DefaultParams().VerifierSlashAmount
	require.Equal(t, slash, committedBond(t, f, verifier))

	// The arbiter resolution window and the escalation window both pass;
	// Phase 8 applies the stashed verdict. Delta stays under ContentTTL
	// (testparams: 10m) so the content is not pruned first.
	endBlockerAt(t, f, 25*time.Second)

	require.True(t, committedBond(t, f, verifier).IsZero(),
		"verifier-right verdict must release the commitment")
	require.Equal(t, seededVerifierBond, currentBond(t, f, verifier),
		"no slash on a vindicated verifier")

	record, err := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.True(t, record.CommitmentReleased)

	content, err := f.keeper.Content.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, content.Status)
}

// The UPHELD path must NOT double-release: SlashBond already frees the
// commitment, and a second saturating ReleaseBond would silently eat a
// different concurrent commitment of the same verifier.
func TestVerdictUpheldDoesNotDoubleRelease(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "vu-peer")

	// Quorum hash != verifier hash → VERIFIER_WRONG.
	trueHash := sha256.Sum256([]byte("the real content vu-x"))
	verifier, _, contentID := challengeQuorum(t, f, ms, "vu-peer", "vu-x", trueHash[:])

	slash := types.DefaultParams().VerifierSlashAmount
	require.Equal(t, slash, committedBond(t, f, verifier))

	endBlockerAt(t, f, 25*time.Second)

	// SlashBond decremented both current and committed by the slash
	// amount. No ReleaseBond is issued on this path — the slash IS how
	// the commitment is freed — and the record is stamped settled so a
	// later walk cannot release on top of it.
	require.True(t, committedBond(t, f, verifier).IsZero())
	require.Equal(t, seededVerifierBond.Sub(slash), currentBond(t, f, verifier))

	record, err := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.True(t, record.CommitmentReleased,
		"the slash settled the commitment, so the record must read as settled -- "+
			"that flag is what stops a later stale-queue walk from releasing on top")
}

// The stale-queue interleaving the CommitmentReleased flag exists to
// close, reachable on DEFAULT params: challenge -> escalate -> early
// OpsComm UPHELD clears PendingVerifierVerdict and tears down the
// EscalatedChallenge, so when the arbiter resolution window later expires
// Phase 7 walks a queue entry nobody removed and sees a record that looks
// like a no-quorum exit. Before the fix that issued a second, saturating
// ReleaseBond which silently ate a DIFFERENT live commitment of the same
// verifier -- which is exactly what this test's second verification is
// here to detect.
func TestEarlyUpheldDoesNotReleaseViaStaleArbiterQueue(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "eo-peer")

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Less(t, params.ArbiterResolutionWindow, 60*time.Second,
		"test assumes the resolution window is still open when the verdict lands")

	opStr := registerTestBridge(t, f, ms, "eo-peer", "eo-op")
	hash := sha256.Sum256([]byte("eo content"))
	contentID := submitTestContent(t, f, ms, opStr, "eo-peer", hash[:])

	verifier := bondTestVerifier(t, f, ms, "eo-verif")
	_, err = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifier, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)

	challenger := testAddr(t, f, "eo-challenger")
	other := sha256.Sum256([]byte("different bytes entirely"))
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: challenger, ContentId: contentID,
		ContentHash: other[:], Evidence: "modified",
	})
	require.NoError(t, err)

	_, err = ms.EscalateChallenge(f.ctx, &types.MsgEscalateChallenge{
		Creator: challenger, ContentId: contentID,
	})
	require.NoError(t, err)
	if _, gerr := f.keeper.EscalatedChallenges.Get(f.ctx, contentID); gerr != nil {
		// Standalone fixture without the service keeper wired: stand up the
		// jury lifecycle record the resolve handler requires.
		require.NoError(t, f.keeper.EscalatedChallenges.Set(f.ctx, contentID, types.EscalatedChallenge{
			ContentId: contentID, Escalator: challenger,
			JuryDeadline:          sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix() + 3600,
			EscrowedEscalationFee: math.ZeroInt(),
		}))
	}

	slash := params.VerifierSlashAmount
	require.Equal(t, slash, committedBond(t, f, verifier))

	// OpsComm resolves in the same block -- long before the arbiter
	// resolution window closes, so its queue entry is still live.
	_, err = ms.ResolveEscalatedChallenge(f.ctx, &types.MsgResolveEscalatedChallenge{
		Authority: f.authority, ContentId: contentID,
		Verdict:   types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD,
		Reasoning: "verifier was wrong",
	})
	require.NoError(t, err)

	require.True(t, committedBond(t, f, verifier).IsZero(), "SlashBond freed the commitment")
	rec, err := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.True(t, rec.CommitmentReleased, "the slash settles the record")

	// A second, unrelated verification by the same verifier: the live
	// commitment a stray release would consume.
	otherOp := registerTestBridge(t, f, ms, "eo-peer", "eo-other-op")
	oh := sha256.Sum256([]byte("unrelated eo content"))
	otherID := submitTestContent(t, f, ms, otherOp, "eo-peer", oh[:])
	_, err = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifier, ContentId: otherID, ContentHash: oh[:],
	})
	require.NoError(t, err)
	require.Equal(t, slash, committedBond(t, f, verifier), "one live commitment")

	// Past the arbiter resolution deadline: Phase 7 walks the stale entry.
	endBlockerAt(t, f, params.ArbiterResolutionWindow+time.Second)

	require.Equal(t, slash, committedBond(t, f, verifier),
		"the unrelated live commitment must survive the stale-queue walk")
}

// The same stale-queue hazard reached the other way: a governance change
// that makes the escalation window SHORTER than the resolution window lets
// Phase 8 apply the auto-verdict in an earlier block than the Phase 7 walk.
// Params.Validate permits that ordering deliberately, so the fix cannot
// rely on it.
func TestUpheldDoesNotReleaseWhenEscalationWindowIsShorter(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	p, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	p.ArbiterResolutionWindow = 60 * time.Second
	p.ArbiterEscalationWindow = 10 * time.Second
	require.NoError(t, p.Validate(), "escalation < resolution is a permitted configuration")
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))

	registerTestPeer(t, f, ms, "dr-peer")
	trueHash := sha256.Sum256([]byte("the real content dr-x"))
	verifier, _, _ := challengeQuorum(t, f, ms, "dr-peer", "dr-x", trueHash[:])

	slash := p.VerifierSlashAmount
	require.Equal(t, slash, committedBond(t, f, verifier))

	// Block A: escalation window expired -> Phase 8 applies UPHELD.
	endBlockerAt(t, f, 15*time.Second)
	require.True(t, committedBond(t, f, verifier).IsZero())

	// A second, unrelated live commitment.
	otherOp := registerTestBridge(t, f, ms, "dr-peer", "dr-other-op")
	oh := sha256.Sum256([]byte("unrelated dr content"))
	otherID := submitTestContent(t, f, ms, otherOp, "dr-peer", oh[:])
	_, err = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifier, ContentId: otherID, ContentHash: oh[:],
	})
	require.NoError(t, err)
	require.Equal(t, slash, committedBond(t, f, verifier))

	// Block B: Phase 7 walks the stale ArbiterResolutionQueue entry.
	endBlockerAt(t, f, 65*time.Second)

	require.Equal(t, slash, committedBond(t, f, verifier),
		"the unrelated live commitment must survive the stale-queue walk")
}

// A challenge that reaches no arbiter quorum produced no finding, so the
// verification stands: CHALLENGED reverts to VERIFIED, matching what
// applyJuryVerdictTimeout already does for the same condition on the jury
// side. Sending it to UNRESOLVED instead would let anyone permanently
// demote any verified content for the price of one challenge fee.
func TestNoQuorumChallengeRevertsToVerified(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "nq-peer")

	opStr := registerTestBridge(t, f, ms, "nq-peer", "nq-op")
	hash := sha256.Sum256([]byte("nq content"))
	contentID := submitTestContent(t, f, ms, opStr, "nq-peer", hash[:])

	verifier := bondTestVerifier(t, f, ms, "nq-verif")
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifier, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)

	challenger := testAddr(t, f, "nq-challenger")
	other := sha256.Sum256([]byte("a griefer's hash"))
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: challenger, ContentId: contentID,
		ContentHash: other[:], Evidence: "drive-by challenge, then silence",
	})
	require.NoError(t, err)

	// No arbiters, no escalation. Let the resolution window expire.
	endBlockerAt(t, f, 16*time.Second)

	content, err := f.keeper.Content.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, content.Status,
		"a challenge that produced no quorum produced no finding; the verification stands")

	require.True(t, committedBond(t, f, verifier).IsZero(),
		"and the verifier's commitment comes back")
}

// UNRESOLVED is system-assigned and terminal: OpsComm must not be able to
// moderate it into a VERIFIED status nobody verified.
func TestModerateRejectsUnresolvedContent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "mu-peer")

	opStr := registerTestBridge(t, f, ms, "mu-peer", "mu-op")
	hash := sha256.Sum256([]byte("mu content"))
	contentID := submitTestContent(t, f, ms, opStr, "mu-peer", hash[:])

	verifier := bondTestVerifier(t, f, ms, "mu-verif")
	mismatch := sha256.Sum256([]byte("what the verifier actually saw"))
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifier, ContentId: contentID, ContentHash: mismatch[:],
	})
	require.NoError(t, err)

	endBlockerAt(t, f, 16*time.Second)

	content, err := f.keeper.Content.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_UNRESOLVED, content.Status)

	_, err = ms.ModerateContent(f.ctx, &types.MsgModerateContent{
		Authority: f.authority, ContentId: contentID,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED,
		Reason:    "laundering an unverified anchor",
	})
	require.Error(t, err, "UNRESOLVED is terminal; moderation must refuse it")
	// The guard has its own error rather than borrowing ErrInvalidParamValue,
	// which appended the misleading "operational or governance param outside
	// valid range" to a rejection that has nothing to do with params.
	require.ErrorIs(t, err, types.ErrContentTerminal)
	require.NotContains(t, err.Error(), "param outside valid range")

	content, err = f.keeper.Content.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_UNRESOLVED, content.Status)
}

// The slash amount is the reservation snapshot, not the live param: a
// mid-dispute governance change must not free more or less bond than
// MsgVerifyContent actually reserved.
func TestUpheldSlashesTheReservationSnapshot(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "ss-peer")

	p, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	reserved := p.VerifierSlashAmount

	trueHash := sha256.Sum256([]byte("the real content ss-x"))
	verifier, _, _ := challengeQuorum(t, f, ms, "ss-peer", "ss-x", trueHash[:])
	require.Equal(t, reserved, committedBond(t, f, verifier))
	before := currentBond(t, f, verifier)

	// Governance doubles the slash amount mid-dispute.
	p.VerifierSlashAmount = reserved.MulRaw(2)
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))

	endBlockerAt(t, f, 25*time.Second)

	require.Equal(t, before.Sub(reserved), currentBond(t, f, verifier),
		"the slash must be the amount that was reserved, not the new param")
	require.True(t, committedBond(t, f, verifier).IsZero(),
		"and it must leave the committed counter exactly at zero")
}

// Row 2: no quorum by the arbiter resolution window — the content goes
// terminally UNRESOLVED and the commitment is released.
func TestNoQuorumMismatchGoesUnresolvedAndReleases(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "nq-peer")
	opStr := registerTestBridge(t, f, ms, "nq-peer", "nq-op")

	hash := sha256.Sum256([]byte("no quorum original"))
	contentID := submitTestContent(t, f, ms, opStr, "nq-peer", hash[:])

	verifier := bondTestVerifier(t, f, ms, "nq-verif")
	wrong := sha256.Sum256([]byte("tampered"))
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifier, ContentId: contentID, ContentHash: wrong[:],
	})
	require.NoError(t, err)

	content, _ := f.keeper.Content.Get(f.ctx, contentID)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED, content.Status)
	require.True(t, committedBond(t, f, verifier).IsPositive())

	// The arbiter window passes with zero submissions.
	endBlockerAt(t, f, 20*time.Second)

	require.True(t, committedBond(t, f, verifier).IsZero(),
		"no-quorum expiry must release the commitment")

	record, err := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.True(t, record.CommitmentReleased)
	require.Positive(t, record.LastChallengeResolvedAt)

	content, err = f.keeper.Content.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_UNRESOLVED, content.Status)
}

// Row 3: challenged, escalated to jury, jury never resolves — the
// timeout path must release the commitment, and the Phase 7 walk must
// NOT misread the escalated record as no-quorum (the EscalatedChallenges
// guard).
func TestJuryTimeoutReleasesCommitment(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "jt-peer")

	verifierHash := sha256.Sum256([]byte("quorum content jt-x"))
	verifier, challenger, contentID := challengeQuorum(t, f, ms, "jt-peer", "jt-x", verifierHash[:])
	require.True(t, committedBond(t, f, verifier).IsPositive())

	// Escalate before the escalation window closes, clearing the stashed
	// verdict and opening the jury lifecycle.
	_, err := ms.EscalateChallenge(f.ctx, &types.MsgEscalateChallenge{
		Creator: challenger, ContentId: contentID,
	})
	require.NoError(t, err)

	// Past the arbiter queue deadline (Phase 7 must skip this record
	// because an EscalatedChallenge exists) and past the jury deadline —
	// TIMEOUT applies. All three windows (15s/20s/15s in testparams) are
	// behind us at +25s, still under the 10m ContentTTL.
	endBlockerAt(t, f, 25*time.Second)

	require.True(t, committedBond(t, f, verifier).IsZero(),
		"jury timeout reaches no verdict against the verifier; the commitment returns")

	record, err := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.True(t, record.CommitmentReleased)

	content, err := f.keeper.Content.Get(f.ctx, contentID)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, content.Status,
		"jury timeout reverts CHALLENGED content to VERIFIED")

	_, err = f.keeper.EscalatedChallenges.Get(f.ctx, contentID)
	require.Error(t, err, "jury lifecycle entry must be torn down")
}

// The practical lockout symptom: N mismatch disputes without quorum must
// not strand commitments — at 50 committed per event against a 500 bond,
// ten events used to lock the verifier out of any further verification.
func TestVerifierCanVerifyAfterRepeatedDisputes(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "rep-peer")
	opStr := registerTestBridge(t, f, ms, "rep-peer", "rep-op")

	verifier := bondTestVerifier(t, f, ms, "rep-verif")
	wrong := sha256.Sum256([]byte("always wrong"))
	for i := 0; i < 10; i++ {
		hash := sha256.Sum256([]byte(fmt.Sprintf("lockout content %d", i)))
		contentID := submitTestContent(t, f, ms, opStr, "rep-peer", hash[:])
		_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
			Creator: verifier, ContentId: contentID, ContentHash: wrong[:],
		})
		require.NoError(t, err, "iteration %d", i)
	}

	// All ten arbiter windows expire without quorum in one sweep.
	endBlockerAt(t, f, 20*time.Second)

	require.True(t, committedBond(t, f, verifier).IsZero(),
		"ten no-quorum disputes must not accumulate commitment")

	// The verifier can still verify: pre-fix this ReserveBond call failed
	// with ErrInsufficientVerifierBond (500 committed of a 500 bond).
	finalHash := sha256.Sum256([]byte("lockout final"))
	finalID := submitTestContent(t, f, ms, opStr, "rep-peer", finalHash[:])
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifier, ContentId: finalID, ContentHash: finalHash[:],
	})
	require.NoError(t, err, "verifier must not be locked out after N disputes")
	require.Equal(t, types.DefaultParams().VerifierSlashAmount, committedBond(t, f, verifier))
}

// P0.4: the list query's peer/type/creator/status filters.
func TestListFederatedContentFilters(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)

	registerTestPeer(t, f, ms, "filt-peer-a")
	registerTestPeer(t, f, ms, "filt-peer-b")
	opA := registerTestBridge(t, f, ms, "filt-peer-a", "filt-op-a")
	opB := registerTestBridge(t, f, ms, "filt-peer-b", "filt-op-b")

	hash1 := sha256.Sum256([]byte("filter 1"))
	id1 := submitTestContent(t, f, ms, opA, "filt-peer-a", hash1[:])
	hash2 := sha256.Sum256([]byte("filter 2"))
	id2 := submitTestContent(t, f, ms, opB, "filt-peer-b", hash2[:])

	// Both pending.
	resp, err := qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{
		PeerId: "filt-peer-a",
	})
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)
	require.Equal(t, id1, resp.Content[0].Id)

	// The verifier runner's poll shape: peer + status.
	resp, err = qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{
		PeerId: "filt-peer-b",
		Status: "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION",
	})
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)
	require.Equal(t, id2, resp.Content[0].Id)

	// Status filter that matches nothing on that peer after verification.
	verifier := bondTestVerifier(t, f, ms, "filt-verif")
	_, err = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifier, ContentId: id1, ContentHash: hash1[:],
	})
	require.NoError(t, err)
	resp, err = qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{
		PeerId: "filt-peer-a",
		Status: "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION",
	})
	require.NoError(t, err)
	require.Empty(t, resp.Content)

	resp, err = qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{
		PeerId: "filt-peer-a",
		Status: "FEDERATED_CONTENT_STATUS_VERIFIED",
	})
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)

	// Type filter through ContentByType.
	resp, err = qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{
		ContentType: "blog_post",
	})
	require.NoError(t, err)
	require.Len(t, resp.Content, 2)

	// Unknown status name is rejected, not silently ignored.
	_, err = qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{
		Status: "VERIFIED",
	})
	require.Error(t, err)

	// No filters still returns everything.
	resp, err = qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Content, 2)
}

// Filtered pagination is denominated in MATCHES, not in index entries
// scanned. A caller can only observe matches, so an offset counted in
// candidates is unusable: paging by offset += len(results) would re-read
// rows already returned, and Total would report "entries examined before
// the page filled" rather than a total. This walks a peer whose content is
// mostly non-matching so the two denominations cannot coincide.
func TestListFederatedContentPaginationIsInMatchSpace(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)

	registerTestPeer(t, f, ms, "pg-peer")
	op := registerTestBridge(t, f, ms, "pg-peer", "pg-op")
	verifier := bondTestVerifier(t, f, ms, "pg-verif")

	// 9 items on the peer; every third stays PENDING_VERIFICATION, the
	// rest are verified away. 3 matches interleaved among 9 candidates.
	var wantPending []uint64
	for i := 0; i < 9; i++ {
		h := sha256.Sum256([]byte(fmt.Sprintf("pg content %d", i)))
		id := submitTestContent(t, f, ms, op, "pg-peer", h[:])
		if i%3 == 0 {
			wantPending = append(wantPending, id)
			continue
		}
		_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
			Creator: verifier, ContentId: id, ContentHash: h[:],
		})
		require.NoError(t, err)
	}
	require.Len(t, wantPending, 3)

	const pending = "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"

	// Total is the match count, not the candidate count.
	resp, err := qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{
		PeerId: "pg-peer", Status: pending,
	})
	require.NoError(t, err)
	require.Len(t, resp.Content, 3)
	require.Equal(t, uint64(3), resp.Pagination.Total,
		"Total must count matches, not the 9 index entries walked")

	// Page through in match space and reassemble the full set with no
	// duplicates and no gaps.
	var paged []uint64
	for offset := uint64(0); offset < resp.Pagination.Total; offset += 2 {
		page, err := qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{
			PeerId: "pg-peer", Status: pending,
			Pagination: &query.PageRequest{Offset: offset, Limit: 2},
		})
		require.NoError(t, err)
		require.Equal(t, uint64(3), page.Pagination.Total, "Total must not vary with the page")
		for _, c := range page.Content {
			paged = append(paged, c.Id)
		}
	}
	require.ElementsMatch(t, wantPending, paged,
		"paging must return each match exactly once")

	// An offset past the end is empty, not a re-read of page one.
	page, err := qs.ListFederatedContent(f.ctx, &types.QueryListFederatedContentRequest{
		PeerId: "pg-peer", Status: pending,
		Pagination: &query.PageRequest{Offset: 3, Limit: 2},
	})
	require.NoError(t, err)
	require.Empty(t, page.Content)
	require.Equal(t, uint64(3), page.Pagination.Total)
}
