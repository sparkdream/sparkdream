package keeper_test

import (
	"crypto/sha256"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

	// Bond commitment and the verification counter BOTH live on x/rep now:
	// the bond on BondedRole, the counter on the shared RoleActivity under
	// the federation_verify action kind. Federation stores neither.
	br, err := f.repKeeper.GetBondedRole(f.ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifierStr)
	require.NoError(t, err)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	require.True(t, committed.IsPositive())

	ra, err := f.repKeeper.GetRoleActivity(f.ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifierStr)
	require.NoError(t, err)
	require.Equal(t, uint64(1), ra.TotalActions[reptypes.ActionKindFederationVerify])
	require.Equal(t, uint64(1), ra.EpochActions[reptypes.ActionKindFederationVerify])
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
			CurrentBond:        types.DefaultParams().MinVerifierBond.String(),
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

// TestVerifyContentBlockedByRepOverturnCooldown pins the ownership split: the
// lockout after a lost challenge lives on x/rep's shared RoleActivity, not on
// a federation-local field, so it applies to the ROLE rather than to this one
// surface. Federation reads it at action time.
func TestVerifyContentBlockedByRepOverturnCooldown(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "cooldown-peer")
	opStr := registerTestBridge(t, f, ms, "cooldown-peer", "cooldown-op")

	hash := sha256.Sum256([]byte("cooldown-body"))
	contentID := submitTestContent(t, f, ms, opStr, "cooldown-peer", hash[:])
	verifierStr := bondTestVerifier(t, f, ms, "cooldown-verifier")

	// An overturned verdict reported to rep starts the shared cooldown.
	f.repKeeper.blockTime = sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix()
	require.NoError(t, f.repKeeper.RecordRoleOutcome(f.ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifierStr,
		reptypes.ActionKindFederationVerify, false))
	require.Positive(t, f.repKeeper.RoleOverturnCooldownUntil(f.ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifierStr))

	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.ErrorIs(t, err, types.ErrVerifierOverturnCooldown)
}
