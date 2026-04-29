package keeper_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"cosmossdk.io/collections"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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

// FEDERATION-S2-5: anonymous (shield-dispatched) submissions must each get a
// unique entry in ArbiterSubmissions and each must increment ArbiterHashCounts
// once. Prior to the fix all anonymous calls collapsed to the shield module
// address, so subsequent calls overwrote each other while the count kept
// climbing. This test pins the corrected behavior.
func TestSubmitArbiterHashAnonymousUniqueKeys(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "anon-arb-peer")
	opStr := registerTestBridge(t, f, ms, "anon-arb-peer", "anon-arb-op")

	hash := sha256.Sum256([]byte("anon arbiter content"))
	contentID := submitTestContent(t, f, ms, opStr, "anon-arb-peer", hash[:])

	verifierStr := bondTestVerifier(t, f, ms, "anon-arb-verif")
	_, _ = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})

	challengerStr := testAddr(t, f, "anon-arb-chal")
	diffHash := sha256.Sum256([]byte("different"))
	_, _ = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{
		Creator: challengerStr, ContentId: contentID, ContentHash: diffHash[:],
		Evidence: "content changed",
	})

	// Two distinct anonymous submissions (shield is the proto signer for both;
	// in production the per-identity dedup is handled by shield's per-content
	// nullifier scope so we can simulate two callers here without modeling it).
	shieldAddr := authtypes.NewModuleAddress("shield")
	shieldStr, err := f.addressCodec.BytesToString(shieldAddr)
	require.NoError(t, err)

	_, err = ms.SubmitArbiterHash(f.ctx, &types.MsgSubmitArbiterHash{
		Creator: shieldStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)
	_, err = ms.SubmitArbiterHash(f.ctx, &types.MsgSubmitArbiterHash{
		Creator: shieldStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)

	// Both calls produced their own ArbiterSubmissions entry (no overwrite).
	var submissionCount int
	err = f.keeper.ArbiterSubmissions.Walk(f.ctx,
		collections.NewPrefixedPairRange[uint64, string](contentID),
		func(_ collections.Pair[uint64, string], _ types.ArbiterHashSubmission) (bool, error) {
			submissionCount++
			return false, nil
		})
	require.NoError(t, err)
	require.Equal(t, 2, submissionCount, "expected two distinct anonymous submission entries")

	// Hash count reflects two votes for the same hash.
	count, err := f.keeper.ArbiterHashCounts.Get(f.ctx,
		collections.Join(contentID, hex.EncodeToString(hash[:])))
	require.NoError(t, err)
	require.Equal(t, uint32(2), count)

	// Sanity: shield module address is not in BridgeOperators (so the
	// identified path's BridgeNotFound error is not what's letting it through).
	_, err = f.keeper.BridgeOperators.Get(f.ctx, collections.Join(shieldStr, "anon-arb-peer"))
	require.Error(t, err)
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
