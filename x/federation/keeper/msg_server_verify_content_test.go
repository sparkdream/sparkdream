package keeper_test

import (
	"crypto/sha256"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

func TestVerifyContentMatch(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "verify-peer")
	opStr := registerTestBridge(t, f, ms, "verify-peer", "verify-op")

	hash := sha256.Sum256([]byte("verified content"))
	contentID := submitTestContent(t, f, ms, opStr, "verify-peer", hash[:])

	verifierStr := bondTestVerifier(t, f, ms, "match-verifier")

	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)

	content, _ := f.keeper.Content.Get(f.ctx, contentID)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, content.Status)

	record, _ := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.Equal(t, verifierStr, record.Verifier)

	// Generic bond commitment lives on rep's BondedRole; per-module counters
	// live on federation's VerifierActivity.
	br, err := f.repKeeper.GetBondedRole(f.ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifierStr)
	require.NoError(t, err)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	require.True(t, committed.IsPositive())

	activity, _ := f.keeper.VerifierActivity.Get(f.ctx, verifierStr)
	require.Equal(t, uint64(1), activity.TotalVerifications)
}

func TestVerifyContentMismatch(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "dispute-peer")
	opStr := registerTestBridge(t, f, ms, "dispute-peer", "dispute-op")

	hash := sha256.Sum256([]byte("original"))
	contentID := submitTestContent(t, f, ms, opStr, "dispute-peer", hash[:])

	verifierStr := bondTestVerifier(t, f, ms, "mismatch-verif")
	wrongHash := sha256.Sum256([]byte("different"))

	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: wrongHash[:],
	})
	require.NoError(t, err)

	content, _ := f.keeper.Content.Get(f.ctx, contentID)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED, content.Status)
}

func TestVerifyContentSelfVerification(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "self-peer")
	opStr := registerTestBridge(t, f, ms, "self-peer", "self-op")

	hash := sha256.Sum256([]byte("self verify"))
	contentID := submitTestContent(t, f, ms, opStr, "self-peer", hash[:])

	// Bond the operator as verifier directly in the mock rep keeper.
	f.repKeeper.SeedBondedRole(
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, opStr,
		reptypes.BondedRole{
			Address:            opStr,
			RoleType:           reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
			BondStatus:         reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			CurrentBond:        types.DefaultMinVerifierBond.String(),
			TotalCommittedBond: "0",
		},
	)

	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: opStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "verifier cannot verify")
}

func TestVerifyContentFirstVerifierWins(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "race-peer")
	opStr := registerTestBridge(t, f, ms, "race-peer", "race-op")

	hash := sha256.Sum256([]byte("race content"))
	contentID := submitTestContent(t, f, ms, opStr, "race-peer", hash[:])

	v1 := bondTestVerifier(t, f, ms, "race-verif1")
	v2 := bondTestVerifier(t, f, ms, "race-verif2")

	// First wins
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: v1, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)

	// Second fails
	_, err = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: v2, ContentId: contentID, ContentHash: hash[:],
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in PENDING_VERIFICATION")
}
