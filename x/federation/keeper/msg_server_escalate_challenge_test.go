package keeper_test

import (
	"crypto/sha256"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestEscalateChallenge(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "esc-peer")
	opStr := registerTestBridge(t, f, ms, "esc-peer", "esc-op")

	hash := sha256.Sum256([]byte("escalate content"))
	contentID := submitTestContent(t, f, ms, opStr, "esc-peer", hash[:])

	verifierStr := bondTestVerifier(t, f, ms, "esc-verifier")
	_, _ = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})

	challengerStr := testAddr(t, f, "esc-challenger")
	diffHash := sha256.Sum256([]byte("different"))
	_, _ = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: challengerStr, ContentId: contentID, ContentHash: diffHash[:],
	})

	// Quorum stashed a verdict and opened the escalation window.
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix()
	stashAutoVerdict(t, f, contentID, now+60)

	// Verifier escalates
	_, err := ms.EscalateChallenge(f.ctx, &types.MsgEscalateChallenge{
		Creator: verifierStr, ContentId: contentID,
	})
	require.NoError(t, err)
}

// stashAutoVerdict stands in for an arbiter quorum: it records a
// VERIFIER_WRONG auto-verdict with the given escalation deadline.
func stashAutoVerdict(t *testing.T, f *fixture, contentID uint64, deadline int64) {
	t.Helper()
	record, err := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.NoError(t, err)
	record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_WRONG
	record.ArbiterEscalationDeadline = deadline
	require.NoError(t, f.keeper.VerificationRecords.Set(f.ctx, contentID, record))
}

// FED-21: the escalation window is enforced even when the EndBlocker has
// not yet drained the escalation queue.
func TestEscalateChallengeAfterDeadline(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "esc3-peer")
	opStr := registerTestBridge(t, f, ms, "esc3-peer", "esc3-op")
	hash := sha256.Sum256([]byte("escalate3"))
	contentID := submitTestContent(t, f, ms, opStr, "esc3-peer", hash[:])
	verifierStr := bondTestVerifier(t, f, ms, "esc3-verifier")
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{Creator: verifierStr, ContentId: contentID, ContentHash: hash[:]})
	require.NoError(t, err)
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{Creator: testAddr(t, f, "esc3-chal"), ContentId: contentID})
	require.NoError(t, err)

	now := sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix()
	stashAutoVerdict(t, f, contentID, now-1)
	_, err = ms.EscalateChallenge(f.ctx, &types.MsgEscalateChallenge{Creator: verifierStr, ContentId: contentID})
	require.ErrorIs(t, err, types.ErrEscalationWindowExpired)
}

// REP-R-07: a verifier who migrated keys can still escalate the record
// written under the old address; the tombstoned old key cannot.
func TestEscalateChallengeMigratedParty(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "esc4-peer")
	opStr := registerTestBridge(t, f, ms, "esc4-peer", "esc4-op")
	hash := sha256.Sum256([]byte("escalate4"))
	contentID := submitTestContent(t, f, ms, opStr, "esc4-peer", hash[:])
	verifierStr := bondTestVerifier(t, f, ms, "esc4-verifier")
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{Creator: verifierStr, ContentId: contentID, ContentHash: hash[:]})
	require.NoError(t, err)
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{Creator: testAddr(t, f, "esc4-chal"), ContentId: contentID})
	require.NoError(t, err)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix()
	stashAutoVerdict(t, f, contentID, now+60)

	newKey := testAddr(t, f, "esc4-verifier-new")
	f.repKeeper.aliases = map[string]string{verifierStr: newKey}

	_, err = ms.EscalateChallenge(f.ctx, &types.MsgEscalateChallenge{Creator: verifierStr, ContentId: contentID})
	require.ErrorIs(t, err, types.ErrNotChallengeParty)
	_, err = ms.EscalateChallenge(f.ctx, &types.MsgEscalateChallenge{Creator: newKey, ContentId: contentID})
	require.NoError(t, err)
}

func TestEscalateChallengeNotParty(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "esc2-peer")
	opStr := registerTestBridge(t, f, ms, "esc2-peer", "esc2-op")

	hash := sha256.Sum256([]byte("escalate2"))
	contentID := submitTestContent(t, f, ms, opStr, "esc2-peer", hash[:])

	verifierStr := bondTestVerifier(t, f, ms, "esc2-verifier")
	_, _ = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})

	challengerStr := testAddr(t, f, "esc2-challeng")
	diffHash := sha256.Sum256([]byte("different2"))
	_, _ = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: challengerStr, ContentId: contentID, ContentHash: diffHash[:],
	})

	// Random third party can't escalate
	randomStr := testAddr(t, f, "esc2-random")
	_, err := ms.EscalateChallenge(f.ctx, &types.MsgEscalateChallenge{
		Creator: randomStr, ContentId: contentID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "challenger or verifier")
}
