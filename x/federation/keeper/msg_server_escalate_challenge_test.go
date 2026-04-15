package keeper_test

import (
	"crypto/sha256"
	"testing"

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

	// Verifier escalates
	_, err := ms.EscalateChallenge(f.ctx, &types.MsgEscalateChallenge{
		Creator: verifierStr, ContentId: contentID,
	})
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
