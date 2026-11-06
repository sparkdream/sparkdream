package keeper_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// cleanupTestTime is a fixed block time so the TTL queues the EndBlocker
// walks behave the same on every run.
var cleanupTestTime = time.Unix(1_700_000_000, 0)

func cleanupCtx(f *fixture) sdk.Context {
	return sdk.UnwrapSDKContext(f.ctx).WithBlockTime(cleanupTestTime).WithEventManager(sdk.NewEventManager())
}

// queuePeerRemoval marks a peer REMOVED and queues it for cleanup directly,
// bypassing MsgRemovePeer's live-bridge precondition (standalone fixtures
// have no x/service to unbond through). The cleanup walk prunes the
// leftover binding itself.
func queuePeerRemoval(t *testing.T, f *fixture, ctx sdk.Context, peerID string) {
	t.Helper()
	peer, err := f.keeper.Peers.Get(ctx, peerID)
	require.NoError(t, err)
	peer.Status = types.PeerStatus_PEER_STATUS_REMOVED
	peer.RemovedAt = ctx.BlockTime().Unix()
	require.NoError(t, f.keeper.SetPeer(ctx, peer))
	require.NoError(t, f.keeper.PeerRemovalQueue.Set(ctx, peerID, types.PeerRemovalState{RemovedAt: peer.RemovedAt}))
}

func runEndBlockerUntilPeerGone(t *testing.T, f *fixture, ctx sdk.Context, peerID string) int {
	t.Helper()
	for i := 1; i <= 50; i++ {
		require.NoError(t, f.keeper.EndBlocker(ctx))
		if has, _ := f.keeper.PeerRemovalQueue.Has(ctx, peerID); !has {
			return i
		}
	}
	t.Fatalf("peer %s cleanup did not finish", peerID)
	return 0
}

func hasEvent(ctx sdk.Context, typ string, attrs map[string]string) bool {
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type != typ {
			continue
		}
		match := true
		for k, v := range attrs {
			attr, ok := ev.GetAttribute(k)
			if !ok || attr.Value != v {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// --- Bug: ContentByHash never pruned ---

func TestPruneExpiredContentRemovesHashIndexAndAllowsReanchor(t *testing.T) {
	f := initFixture(t)
	ctx := cleanupCtx(f)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "aurora.social")
	opStr := registerTestBridge(t, f, ms, "aurora.social", "aurora-op")

	hash := sha256.Sum256([]byte("anchor me twice"))
	hashHex := hex.EncodeToString(hash[:])
	contentID := submitTestContent(t, f, ms, opStr, "aurora.social", hash[:])

	// Expire it.
	content, err := f.keeper.Content.Get(ctx, contentID)
	require.NoError(t, err)
	require.NoError(t, f.keeper.ContentExpiration.Remove(ctx, collections.Join(content.ExpiresAt, contentID)))
	content.ExpiresAt = 1
	require.NoError(t, f.keeper.Content.Set(ctx, contentID, content))
	require.NoError(t, f.keeper.ContentExpiration.Set(ctx, collections.Join(int64(1), contentID)))

	require.NoError(t, f.keeper.EndBlocker(ctx))

	_, err = f.keeper.Content.Get(ctx, contentID)
	require.Error(t, err)
	has, err := f.keeper.ContentByHash.Has(ctx, hashHex)
	require.NoError(t, err)
	require.False(t, has, "hash index must be pruned with the content")
	has, err = f.keeper.ContentByCreator.Has(ctx, collections.Join(content.CreatorIdentity, contentID))
	require.NoError(t, err)
	require.False(t, has)

	// The same content can be anchored again.
	newID := submitTestContent(t, f, ms, opStr, "aurora.social", hash[:])
	require.NotEqual(t, contentID, newID)
	indexed, err := f.keeper.ContentByHash.Get(ctx, hashHex)
	require.NoError(t, err)
	require.Equal(t, newID, indexed)

	// And a live record still dedups.
	_, err = ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		License: "CC0-1.0", Operator: opStr, PeerId: "aurora.social",
		RemoteContentId: "dup", ContentType: "blog_post", ContentHash: hash[:],
		CreatorIdentity: "@test@aurora.social", Title: "t", Body: "b",
	})
	require.ErrorIs(t, err, types.ErrDuplicateContent)
}

// An orphan hash entry (left by a pre-v2 prune) must not block anchoring.
func TestSubmitFederatedContentIgnoresOrphanHashEntry(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "zenith.social")
	opStr := registerTestBridge(t, f, ms, "zenith.social", "zenith-op")

	hash := sha256.Sum256([]byte("orphaned"))
	hashHex := hex.EncodeToString(hash[:])
	require.NoError(t, f.keeper.ContentByHash.Set(f.ctx, hashHex, 9999))

	contentID := submitTestContent(t, f, ms, opStr, "zenith.social", hash[:])
	indexed, err := f.keeper.ContentByHash.Get(f.ctx, hashHex)
	require.NoError(t, err)
	require.Equal(t, contentID, indexed, "orphan entry is overwritten by the new record")
}

// --- Bug: peer removal leaves links, attestations, challenges, disputes ---

func TestPeerRemovalCleansIdentityAndAttestationState(t *testing.T) {
	f := initFixture(t)
	ctx := cleanupCtx(f)
	ms := keeper.NewMsgServerImpl(f.keeper)
	const peerID = "phoenix.social"
	const otherPeer = "nova.social"
	registerTestPeer(t, f, ms, peerID)
	registerTestPeer(t, f, ms, otherPeer)
	opStr := registerTestBridge(t, f, ms, peerID, "phoenix-op")

	// Small budget so every phase has to resume across blocks.
	params, err := f.keeper.Params.Get(ctx)
	require.NoError(t, err)
	params.MaxPrunePerBlock = 2
	require.NoError(t, f.keeper.Params.Set(ctx, params))

	for i := 0; i < 3; i++ {
		h := sha256.Sum256([]byte{byte(i), 'p'})
		submitTestContent(t, f, ms, opStr, peerID, h[:])
	}

	verifiedUser := testAddr(t, f, "verified-user")
	unverifiedUser := testAddr(t, f, "unverified-user")
	otherUser := testAddr(t, f, "other-user")
	// unverifiedUser links through the real handler (sets the expiry queue).
	_, err = ms.LinkIdentity(ctx, &types.MsgLinkIdentity{
		Creator: unverifiedUser, PeerId: peerID, RemoteIdentity: "@unverified@phoenix.social",
	})
	require.NoError(t, err)
	// verifiedUser gets a VERIFIED link on peerID and one on otherPeer.
	seedLink := func(local, peer, remote string, status types.IdentityLinkStatus) {
		require.NoError(t, f.keeper.IdentityLinks.Set(ctx, collections.Join(local, peer), types.IdentityLink{
			LocalAddress: local, PeerId: peer, RemoteIdentity: remote, Status: status, LinkedAt: 1, VerifiedAt: 2,
		}))
		require.NoError(t, f.keeper.IdentityLinksByRemote.Set(ctx, collections.Join(peer, remote), local))
		require.NoError(t, f.keeper.IdentityClaimsByRemote.Set(ctx, collections.Join3(peer, remote, local)))
		cnt, _ := f.keeper.IdentityLinkCount.Get(ctx, local)
		require.NoError(t, f.keeper.IdentityLinkCount.Set(ctx, local, cnt+1))
	}
	seedLink(verifiedUser, peerID, "@verified@phoenix.social", types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED)
	seedLink(verifiedUser, otherPeer, "@verified@nova.social", types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED)

	expiresAt := cleanupTestTime.Unix() + 3600
	for _, local := range []string{verifiedUser, otherUser} {
		for _, peer := range []string{peerID, otherPeer} {
			require.NoError(t, f.keeper.RepAttestations.Set(ctx, collections.Join(local, peer), types.ReputationAttestation{
				LocalAddress: local, PeerId: peer, ExpiresAt: expiresAt,
			}))
			require.NoError(t, f.keeper.AttestationExp.Set(ctx, collections.Join3(expiresAt, local, peer)))
			require.NoError(t, f.keeper.RepAttestationsByPeer.Set(ctx, collections.Join(peer, local)))
		}
	}
	require.NoError(t, f.keeper.SetPendingChallengeForTest(ctx, types.PendingIdentityChallenge{
		ClaimedAddress: otherUser, ClaimantChainPeerId: peerID, ExpiresAt: expiresAt,
	}))
	require.NoError(t, f.keeper.SetPendingChallengeForTest(ctx, types.PendingIdentityChallenge{
		ClaimedAddress: otherUser, ClaimantChainPeerId: otherPeer, ExpiresAt: expiresAt,
	}))
	for id := uint64(1); id <= 5; id++ {
		peer := peerID
		if id%2 == 0 {
			peer = otherPeer
		}
		require.NoError(t, f.keeper.OutboundAttestations.Set(ctx, id, types.OutboundAttestation{Id: id, PeerId: peer}))
	}

	queuePeerRemoval(t, f, ctx, peerID)
	blocks := runEndBlockerUntilPeerGone(t, f, ctx, peerID)
	require.Greater(t, blocks, 3, "budget of 2 must force a multi-block cleanup")

	// Peer and its content gone.
	has, _ := f.keeper.Peers.Has(ctx, peerID)
	require.False(t, has)
	require.True(t, hasEvent(ctx, types.EventTypePeerCleanupComplete, map[string]string{types.AttributeKeyPeerID: peerID}))
	n := 0
	require.NoError(t, f.keeper.ContentByHash.Walk(ctx, nil, func(string, uint64) (bool, error) { n++; return false, nil }))
	require.Zero(t, n, "content hashes of removed peer must be gone")

	// Identity links on the removed peer deleted, counts released; the
	// other peer's link survives.
	for _, local := range []string{verifiedUser, unverifiedUser} {
		has, _ = f.keeper.IdentityLinks.Has(ctx, collections.Join(local, peerID))
		require.False(t, has)
	}
	has, _ = f.keeper.IdentityLinksByRemote.Has(ctx, collections.Join(peerID, "@verified@phoenix.social"))
	require.False(t, has)
	has, _ = f.keeper.IdentityLinks.Has(ctx, collections.Join(verifiedUser, otherPeer))
	require.True(t, has)
	cnt, _ := f.keeper.IdentityLinkCount.Get(ctx, verifiedUser)
	require.Equal(t, uint32(1), cnt)
	cnt, _ = f.keeper.IdentityLinkCount.Get(ctx, unverifiedUser)
	require.Equal(t, uint32(0), cnt)
	require.True(t, hasEvent(ctx, types.EventTypeIdentityLinkRevoked, map[string]string{
		types.AttributeKeyLocalAddress:   verifiedUser,
		types.AttributeKeyPeerID:         peerID,
		types.AttributeKeyRemoteIdentity: "@verified@phoenix.social",
	}))
	require.False(t, hasEvent(ctx, types.EventTypeIdentityLinkRevoked, map[string]string{
		types.AttributeKeyLocalAddress: unverifiedUser,
	}), "unverified links are deleted silently")
	unverifiedExp := 0
	require.NoError(t, f.keeper.UnverifiedLinkExp.Walk(ctx, nil, func(collections.Triple[int64, string, string]) (bool, error) {
		unverifiedExp++
		return false, nil
	}))
	require.Zero(t, unverifiedExp)

	// Attestations, challenges, outbound: only the other peer's remain.
	require.NoError(t, f.keeper.RepAttestations.Walk(ctx, nil, func(k collections.Pair[string, string], _ types.ReputationAttestation) (bool, error) {
		require.Equal(t, otherPeer, k.K2())
		return false, nil
	}))
	require.NoError(t, f.keeper.AttestationExp.Walk(ctx, nil, func(k collections.Triple[int64, string, string]) (bool, error) {
		require.Equal(t, otherPeer, k.K3())
		return false, nil
	}))
	has, _ = f.keeper.PendingIdChallenges.Has(ctx, collections.Join(otherUser, peerID))
	require.False(t, has)
	has, _ = f.keeper.PendingIdChallenges.Has(ctx, collections.Join(otherUser, otherPeer))
	require.True(t, has)
	var outbound []uint64
	require.NoError(t, f.keeper.OutboundAttestations.Walk(ctx, nil, func(id uint64, att types.OutboundAttestation) (bool, error) {
		require.Equal(t, otherPeer, att.PeerId)
		outbound = append(outbound, id)
		return false, nil
	}))
	require.Equal(t, []uint64{2, 4}, outbound)

	// Re-registering the same peer id and linking again works.
	registerTestPeer(t, f, ms, peerID)
	_, err = ms.LinkIdentity(ctx, &types.MsgLinkIdentity{
		Creator: verifiedUser, PeerId: peerID, RemoteIdentity: "@verified@phoenix.social",
	})
	require.NoError(t, err)
	cnt, _ = f.keeper.IdentityLinkCount.Get(ctx, verifiedUser)
	require.Equal(t, uint32(2), cnt)
}

func TestPeerRemovalCancelsActiveChallenge(t *testing.T) {
	f := initFixture(t)
	ctx := cleanupCtx(f)
	ms := keeper.NewMsgServerImpl(f.keeper)
	const peerID = "aurora.social"
	registerTestPeer(t, f, ms, peerID)
	opStr := registerTestBridge(t, f, ms, peerID, "aurora-op")

	hash := sha256.Sum256([]byte("disputed content"))
	contentID := submitTestContent(t, f, ms, opStr, peerID, hash[:])
	verifierStr := bondTestVerifier(t, f, ms, "aurora-verif")
	_, err := ms.VerifyContent(ctx, &types.MsgVerifyContent{Creator: verifierStr, ContentId: contentID, ContentHash: hash[:]})
	require.NoError(t, err)
	challengerStr := testAddr(t, f, "aurora-challenger")
	other := sha256.Sum256([]byte("other"))
	_, err = ms.ChallengeVerification(ctx, &types.MsgChallengeVerification{
		Creator: challengerStr, ContentId: contentID, ContentHash: other[:], Evidence: "modified",
	})
	require.NoError(t, err)

	record, err := f.keeper.VerificationRecords.Get(ctx, contentID)
	require.NoError(t, err)
	require.True(t, record.CommittedAmount.IsPositive())
	fee := record.EscrowedChallengeFee
	require.True(t, fee.IsPositive())
	br, _ := f.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifierStr)
	require.Equal(t, record.CommittedAmount.String(), br.TotalCommittedBond)

	// An open jury escalation is cancelled and its fee refunded too.
	require.NoError(t, f.keeper.EscalatedChallenges.Set(ctx, contentID, types.EscalatedChallenge{
		ContentId: contentID, Escalator: challengerStr, EscrowedEscalationFee: math.NewInt(7), JuryDeadline: cleanupTestTime.Unix() + 100,
	}))
	require.NoError(t, f.keeper.EscalatedChallengeDeadline.Set(ctx, collections.Join(cleanupTestTime.Unix()+100, contentID)))

	queuePeerRemoval(t, f, ctx, peerID)
	runEndBlockerUntilPeerGone(t, f, ctx, peerID)

	require.True(t, hasEvent(ctx, types.EventTypeChallengeCancelledPeerRemoved, map[string]string{
		types.AttributeKeyContentID:          strconv.FormatUint(contentID, 10),
		types.AttributeKeyPeerID:             peerID,
		types.AttributeKeyChallengerRefunded: fee.String(),
	}))
	require.True(t, hasEvent(ctx, types.EventTypeEscalationFeeRefunded, map[string]string{
		types.AttributeKeyAmount: "7",
	}))
	_, err = f.keeper.VerificationRecords.Get(ctx, contentID)
	require.Error(t, err, "verification record deleted")
	has, _ := f.keeper.EscalatedChallenges.Has(ctx, contentID)
	require.False(t, has)
	has, _ = f.keeper.ChallengeWindow.Has(ctx, collections.Join(record.ChallengeWindowEnds, contentID))
	require.False(t, has)
	br, _ = f.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifierStr)
	require.Equal(t, "0", br.TotalCommittedBond, "verifier commitment released")

	// Leftover deadline-keyed queue entries drain harmlessly later.
	later := ctx.WithBlockTime(cleanupTestTime.Add(400 * 24 * time.Hour))
	require.NoError(t, f.keeper.EndBlocker(later))
}
