package keeper_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestChallengeVerification(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "chal-peer")
	opStr := registerTestBridge(t, f, ms, "chal-peer", "chal-op")

	hash := sha256.Sum256([]byte("challenge me"))
	contentID := submitTestContent(t, f, ms, opStr, "chal-peer", hash[:])

	// Verify content first
	verifierStr := bondTestVerifier(t, f, ms, "chal-verifier")
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)

	// Challenge
	challengerStr := testAddr(t, f, "challenger")
	differentHash := sha256.Sum256([]byte("different"))
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: challengerStr, ContentId: contentID,
		ContentHash: differentHash[:], Evidence: "content was modified",
	})
	require.NoError(t, err)

	content, _ := f.keeper.Content.Get(f.ctx, contentID)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED, content.Status)
}

func TestChallengeVerificationSelfChallenge(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "selfchal-peer")
	opStr := registerTestBridge(t, f, ms, "selfchal-peer", "selfchal-op")

	hash := sha256.Sum256([]byte("self challenge"))
	contentID := submitTestContent(t, f, ms, opStr, "selfchal-peer", hash[:])

	verifierStr := bondTestVerifier(t, f, ms, "selfchal-verif")
	_, _ = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})

	// Verifier can't challenge own verification
	_, err := ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "challenger is the verifier")

	// Operator can't challenge either
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: opStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "challenger is the submitting operator")
}
