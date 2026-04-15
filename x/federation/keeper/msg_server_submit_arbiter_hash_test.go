package keeper_test

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestSubmitArbiterHash(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "arbiter-peer")
	opStr := registerTestBridge(t, f, ms, "arbiter-peer", "arbiter-op1")
	op2Str := registerTestBridge(t, f, ms, "arbiter-peer", "arbiter-op2")

	hash := sha256.Sum256([]byte("arbiter content"))
	contentID := submitTestContent(t, f, ms, opStr, "arbiter-peer", hash[:])

	// Verify then challenge
	verifierStr := bondTestVerifier(t, f, ms, "arbiter-verif")
	_, _ = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})

	challengerStr := testAddr(t, f, "arb-challenger")
	diffHash := sha256.Sum256([]byte("different"))
	_, _ = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: challengerStr, ContentId: contentID, ContentHash: diffHash[:],
		Evidence: "content changed",
	})

	// Competing operator submits arbiter hash (identified path)
	_, err := ms.SubmitArbiterHash(f.ctx, &types.MsgSubmitArbiterHash{
		Creator: op2Str, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)
}

func TestSubmitArbiterHashSelfArbiter(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "self-arb-peer")
	opStr := registerTestBridge(t, f, ms, "self-arb-peer", "self-arb-op")

	hash := sha256.Sum256([]byte("self arbiter"))
	contentID := submitTestContent(t, f, ms, opStr, "self-arb-peer", hash[:])

	verifierStr := bondTestVerifier(t, f, ms, "self-arb-verif")
	_, _ = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})

	challengerStr := testAddr(t, f, "self-arb-chal")
	diffHash := sha256.Sum256([]byte("different"))
	_, _ = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: challengerStr, ContentId: contentID, ContentHash: diffHash[:],
	})

	// Submitting operator can't arbitrate own content
	_, err := ms.SubmitArbiterHash(f.ctx, &types.MsgSubmitArbiterHash{
		Creator: opStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot arbitrate")
}
