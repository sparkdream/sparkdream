package keeper_test

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	servicetypes "sparkdream/x/service/types"
)

// Regression tests for the 2026-10 federation review (FED-02 .. FED-24).
// The IBC callback wiring itself (FED-01, destination-channel lookup with
// asymmetric channel ids) is covered in x/federation/module.

// ibcPeer registers an ACTIVE Spark Dream peer bound to channelID with an
// inbound blog_post policy.
func ibcPeer(t *testing.T, f *fixture, peerID, channelID string) {
	t.Helper()
	ms := keeper.NewMsgServerImpl(f.keeper)
	setupIBCPeerForLink(t, f, ms, peerID, channelID)
	require.NoError(t, f.keeper.PeerPolicies.Set(f.ctx, peerID, types.PeerPolicy{
		PeerId: peerID, InboundContentTypes: []string{"blog_post"}, AllowReputationQueries: true,
		AcceptReputationAttestations: true,
	}))
}

func contentPacket(seed byte) *types.ContentPacket {
	return &types.ContentPacket{
		ContentType: "blog_post", RemoteContentId: "r", Creator: "phoenix1creator",
		ContentHash: testHash(seed), License: "CC0-1.0", Body: "b",
	}
}

func setPeerStatus(t *testing.T, f *fixture, peerID string, st types.PeerStatus) {
	t.Helper()
	peer, err := f.keeper.Peers.Get(f.ctx, peerID)
	require.NoError(t, err)
	peer.Status = st
	require.NoError(t, f.keeper.SetPeer(f.ctx, peer))
}

// FED-02: only an ACTIVE peer's packets are accepted. A suspended peer gets
// an error ack; a removed peer's channel is unknown.
func TestInboundRejectsNonActivePeers(t *testing.T) {
	f := initFixture(t)
	ibcPeer(t, f, "phoenix-1", "channel-7")

	require.NoError(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", contentPacket(1)))

	setPeerStatus(t, f, "phoenix-1", types.PeerStatus_PEER_STATUS_SUSPENDED)
	err := f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", contentPacket(2))
	require.ErrorIs(t, err, types.ErrPeerNotActive)
	_, err = f.keeper.OnRecvReputationQueryPacket(f.ctx, types.PortID, "channel-7", &types.ReputationQueryPacket{QueriedAddress: "x"})
	require.ErrorIs(t, err, types.ErrPeerNotActive)

	setPeerStatus(t, f, "phoenix-1", types.PeerStatus_PEER_STATUS_REMOVED)
	err = f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", contentPacket(3))
	require.ErrorIs(t, err, types.ErrUnknownChannel)

	// Wrong port is refused too.
	setPeerStatus(t, f, "phoenix-1", types.PeerStatus_PEER_STATUS_ACTIVE)
	err = f.keeper.OnRecvContentPacket(f.ctx, "transfer", "channel-7", contentPacket(4))
	require.ErrorIs(t, err, types.ErrUnknownChannel)
}

// FED-02: a REMOVED peer's channel id re-bound to a new peer credits the
// new peer, never the removed one.
func TestFindPeerByChannelSkipsRemoved(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.SetPeer(f.ctx, types.Peer{
		Id: "aaa-old", Type: types.PeerType_PEER_TYPE_SPARK_DREAM, Status: types.PeerStatus_PEER_STATUS_REMOVED, IbcChannelId: "channel-9",
	}))
	ibcPeer(t, f, "zenith-1", "channel-9")
	require.NoError(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-9", contentPacket(5)))
	var peers []string
	require.NoError(t, f.keeper.Content.Walk(f.ctx, nil, func(_ uint64, c types.FederatedContent) (bool, error) {
		peers = append(peers, c.PeerId)
		return false, nil
	}))
	require.Equal(t, []string{"zenith-1"}, peers)
}

// FED-05 / FED-14 / FED-16: the IBC content path applies blocked
// identities, a 32-byte hash, identifier caps, UTF-8-safe truncation and the
// inbound rate limits.
func TestInboundContentAdmission(t *testing.T) {
	f := initFixture(t)
	ibcPeer(t, f, "phoenix-1", "channel-7")
	policy, _ := f.keeper.PeerPolicies.Get(f.ctx, "phoenix-1")
	policy.BlockedIdentities = []string{"phoenix1spammer"}
	policy.InboundRateLimitPerEpoch = 3
	require.NoError(t, f.keeper.PeerPolicies.Set(f.ctx, "phoenix-1", policy))

	p := contentPacket(1)
	p.Creator = "phoenix1spammer"
	require.ErrorIs(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", p), types.ErrIdentityBlocked)

	p = contentPacket(1)
	p.ContentHash = nil
	require.ErrorIs(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", p), types.ErrInvalidContentHash)

	p = contentPacket(1)
	p.RemoteContentId = strings.Repeat("r", types.MaxInboundIdentifierSize+1)
	require.ErrorIs(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", p), types.ErrInvalidInboundField)

	params, _ := f.keeper.Params.Get(f.ctx)
	p = contentPacket(1)
	p.Body = strings.Repeat("é", int(params.MaxContentBodySize)) // 2 bytes per rune
	p.Title = strings.Repeat("é", types.MaxInboundTitleSize)
	require.NoError(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", p))
	id, err := f.keeper.ContentByHash.Get(f.ctx, sha256Hex(testHash(1)))
	require.NoError(t, err)
	stored, _ := f.keeper.Content.Get(f.ctx, id)
	require.LessOrEqual(t, uint64(len(stored.Body)), params.MaxContentBodySize)
	require.True(t, utf8.ValidString(stored.Body))
	require.LessOrEqual(t, len(stored.Title), types.MaxInboundTitleSize)
	require.True(t, utf8.ValidString(stored.Title))

	// Per-peer window: 3 accepted in all (one above), the next refused.
	require.NoError(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", contentPacket(2)))
	require.NoError(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", contentPacket(3)))
	require.ErrorIs(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", contentPacket(4)), types.ErrRateLimitExceeded)
}

func sha256Hex(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, hexdigits[c>>4], hexdigits[c&0xf])
	}
	return string(out)
}

// FED-14: truncation never splits a rune.
func TestTruncateUTF8(t *testing.T) {
	s := "aé" // a + 2-byte rune
	require.Equal(t, "a", types.TruncateUTF8(s, 2))
	require.Equal(t, "aé", types.TruncateUTF8(s, 3))
	require.Equal(t, "", types.TruncateUTF8("é", 1))
}

// FED-06 / FED-08: identity challenges are validated, capped per peer,
// indexed by expiry and pruned by the expiry walk; an unknown local address
// answers exists = false and stores nothing.
func TestInboundIdentityChallenge(t *testing.T) {
	f := initFixture(t)
	ibcPeer(t, f, "phoenix-1", "channel-7")
	claimed := testAddr(t, f, "claimed")
	missing := testAddr(t, f, "missing")
	f.authKeeper.missing = map[string]bool{missing: true}

	ack, err := f.keeper.OnRecvIdentityVerificationPacket(f.ctx, types.PortID, "channel-7", &types.IdentityVerificationPacket{
		ClaimedAddress: missing, ClaimantAddress: "phoenix1user", Challenge: testHash(1),
	})
	require.NoError(t, err)
	require.False(t, ack.Exists)
	ack, err = f.keeper.OnRecvIdentityVerificationPacket(f.ctx, types.PortID, "channel-7", &types.IdentityVerificationPacket{
		ClaimedAddress: "not-bech32", ClaimantAddress: "phoenix1user", Challenge: testHash(1),
	})
	require.NoError(t, err)
	require.False(t, ack.Exists)

	ack, err = f.keeper.OnRecvIdentityVerificationPacket(f.ctx, types.PortID, "channel-7", &types.IdentityVerificationPacket{
		ClaimedAddress: claimed, ClaimantAddress: "phoenix1user", Challenge: testHash(1),
	})
	require.NoError(t, err)
	require.True(t, ack.Exists)
	n, _ := f.keeper.PendingIdChallengeCount.Get(f.ctx, "phoenix-1")
	require.Equal(t, uint64(1), n)

	// The cap counts distinct claimed addresses per peer.
	require.NoError(t, f.keeper.PendingIdChallengeCount.Set(f.ctx, "phoenix-1", types.MaxPendingChallengesPerPeer))
	_, err = f.keeper.OnRecvIdentityVerificationPacket(f.ctx, types.PortID, "channel-7", &types.IdentityVerificationPacket{
		ClaimedAddress: testAddr(t, f, "claimed-2"), ClaimantAddress: "phoenix1user", Challenge: testHash(1),
	})
	require.ErrorIs(t, err, types.ErrTooManyChallenges)
	require.NoError(t, f.keeper.PendingIdChallengeCount.Set(f.ctx, "phoenix-1", 1))

	// Expiry walk prunes it once challenge_ttl has passed.
	c, err := f.keeper.PendingIdChallenges.Get(f.ctx, collections.Join(claimed, "phoenix-1"))
	require.NoError(t, err)
	later := f.sdkCtx().WithBlockTime(time.Unix(c.ExpiresAt+1, 0))
	require.NoError(t, f.keeper.EndBlocker(later))
	has, _ := f.keeper.PendingIdChallenges.Has(later, collections.Join(claimed, "phoenix-1"))
	require.False(t, has)
	n, _ = f.keeper.PendingIdChallengeCount.Get(later, "phoenix-1")
	require.Zero(t, n)
}

// FED-08: exists = false revokes the link; a timeout re-sends up to three
// attempts, then gives up.
func TestIdentityVerificationAckAndRetry(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	ibcPeer(t, f, "phoenix-1", "channel-7")
	user := testAddr(t, f, "linker")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{Creator: user, PeerId: "phoenix-1", RemoteIdentity: "phoenix1remote"})
	require.NoError(t, err)
	link, _ := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(user, "phoenix-1"))
	require.Equal(t, uint32(1), link.VerificationAttempts)
	pkt := &types.IdentityVerificationPacket{ClaimedAddress: "phoenix1remote", ClaimantAddress: user, Challenge: link.Challenge}

	// Timeouts: attempts climb to the cap, then identity_verification_failed.
	f.keeper.OnTimeoutIdentityVerification(f.ctx, "channel-7", pkt)
	link, _ = f.keeper.IdentityLinks.Get(f.ctx, collections.Join(user, "phoenix-1"))
	require.Equal(t, uint32(2), link.VerificationAttempts)
	f.keeper.OnTimeoutIdentityVerification(f.ctx, "channel-7", pkt)
	f.keeper.OnTimeoutIdentityVerification(f.ctx, "channel-7", pkt)
	link, _ = f.keeper.IdentityLinks.Get(f.ctx, collections.Join(user, "phoenix-1"))
	require.Equal(t, uint32(types.MaxVerificationAttempts), link.VerificationAttempts)
	require.True(t, hasEvent(f.sdkCtx(), types.EventTypeIdentityVerificationFailed, map[string]string{types.AttributeKeyLocalAddress: user}))

	// exists = false revokes it and frees the user's link slot.
	ackBytes, _ := (&types.IdentityVerificationAck{Exists: false}).Marshal()
	f.keeper.OnAckIdentityVerification(f.ctx, "channel-7", pkt, ackBytes)
	has, _ := f.keeper.IdentityLinks.Has(f.ctx, collections.Join(user, "phoenix-1"))
	require.False(t, has)
	cnt, _ := f.keeper.IdentityLinkCount.Get(f.ctx, user)
	require.Zero(t, cnt)
	has, _ = f.keeper.IdentityLinkCount.Has(f.ctx, user)
	require.False(t, has, "a zero link count is removed, not stored")
	has, _ = f.keeper.IdentityLinksByRemote.Has(f.ctx, collections.Join("phoenix-1", "phoenix1remote"))
	require.False(t, has)
}

// FED-08: the reputation response reports real membership status, join
// time and per-tag reputation.
func TestReputationResponseFilled(t *testing.T) {
	f := initFixture(t)
	ibcPeer(t, f, "phoenix-1", "channel-7")
	member := testAddr(t, f, "member")
	zeroed := testAddr(t, f, "zeroed")
	f.repKeeper.inactive = map[string]bool{zeroed: true}
	f.repKeeper.repScores = map[string]map[string]string{member: {"go": "12.7", "art": "3.0", "neg": "-1"}}

	resp, err := f.keeper.OnRecvReputationQueryPacket(f.ctx, types.PortID, "channel-7", &types.ReputationQueryPacket{QueriedAddress: member})
	require.NoError(t, err)
	require.True(t, resp.IsActive)
	require.Equal(t, int64(1_700_000_000), resp.MemberSince)
	require.Equal(t, []types.TagReputation{{Tag: "art", Score: math.NewInt(3)}, {Tag: "go", Score: math.NewInt(12)}}, resp.Reputations)

	resp, err = f.keeper.OnRecvReputationQueryPacket(f.ctx, types.PortID, "channel-7", &types.ReputationQueryPacket{QueriedAddress: zeroed})
	require.NoError(t, err)
	require.False(t, resp.IsActive)
}

// FED-10: an unattributable or stale reputation ack is dropped (nil error,
// so the packet commitment still clears) and writes nothing.
func TestAckReputationQueryDropsStale(t *testing.T) {
	f := initFixture(t)
	ibcPeer(t, f, "phoenix-1", "channel-7")
	user := testAddr(t, f, "requester")
	require.NoError(t, f.keeper.IdentityLinks.Set(f.ctx, collections.Join(user, "phoenix-1"), types.IdentityLink{
		LocalAddress: user, PeerId: "phoenix-1", RemoteIdentity: "phoenix1me", Status: types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED,
	}))
	q := &types.ReputationQueryPacket{Requester: user, QueriedAddress: "phoenix1me"}
	ok, _ := (&types.ReputationResponseData{Address: "phoenix1me", TrustLevel: 3, IsActive: true}).Marshal()
	other, _ := (&types.ReputationResponseData{Address: "phoenix1someoneelse", TrustLevel: 3, IsActive: true}).Marshal()

	require.NoError(t, f.keeper.OnAckReputationQuery(f.ctx, "channel-7", q, []byte{0xff, 0x01}))
	require.NoError(t, f.keeper.OnAckReputationQuery(f.ctx, "channel-99", q, ok))
	require.NoError(t, f.keeper.OnAckReputationQuery(f.ctx, "channel-7", q, other))
	has, _ := f.keeper.RepAttestations.Has(f.ctx, collections.Join(user, "phoenix-1"))
	require.False(t, has)

	setPeerStatus(t, f, "phoenix-1", types.PeerStatus_PEER_STATUS_SUSPENDED)
	require.NoError(t, f.keeper.OnAckReputationQuery(f.ctx, "channel-7", q, ok))
	has, _ = f.keeper.RepAttestations.Has(f.ctx, collections.Join(user, "phoenix-1"))
	require.False(t, has)

	setPeerStatus(t, f, "phoenix-1", types.PeerStatus_PEER_STATUS_ACTIVE)
	require.NoError(t, f.keeper.OnAckReputationQuery(f.ctx, "channel-7", q, ok))
	att, err := f.keeper.RepAttestations.Get(f.ctx, collections.Join(user, "phoenix-1"))
	require.NoError(t, err)
	require.Equal(t, "phoenix1me", att.RemoteAddress)
}

// FED-03: InitGenesis rebuilds BindingsByOperator, IdentityLinkCount and
// UnverifiedLinkExp, and round-trips pending challenges and the peer
// removal queue; the index invariants hold afterwards.
func TestGenesisRebuildsIndexes(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.social")
	op := registerTestBridge(t, f, ms, "phoenix.social", "gen-op")
	user := testAddr(t, f, "gen-user")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{Creator: user, PeerId: "phoenix.social", RemoteIdentity: "@u@phoenix.social"})
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetPendingChallengeForTest(f.ctx, types.PendingIdentityChallenge{
		ClaimedAddress: user, ClaimantChainPeerId: "phoenix.social", ClaimantAddress: "r", Challenge: []byte("c"), ExpiresAt: 99,
	}))
	require.NoError(t, f.keeper.PeerRemovalQueue.Set(f.ctx, "aurora-gone", types.PeerRemovalState{RemovedAt: 5, ContentDone: true}))

	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.PendingIdentityChallenges, 1)
	require.Len(t, gs.PeerRemovalQueue, 1)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gs))
	has, _ := g.keeper.BindingsByOperator.Has(g.ctx, collections.Join3(types.ServiceTypeFederationBridgeActivityPub, op, "phoenix.social"))
	require.True(t, has, "hooks find bindings only through BindingsByOperator")
	cnt, _ := g.keeper.IdentityLinkCount.Get(g.ctx, user)
	require.Equal(t, uint32(1), cnt)
	link, _ := g.keeper.IdentityLinks.Get(g.ctx, collections.Join(user, "phoenix.social"))
	has, _ = g.keeper.UnverifiedLinkExp.Has(g.ctx, collections.Join3(link.UnverifiedExpiresAt, user, "phoenix.social"))
	require.True(t, has)
	has, _ = g.keeper.PendingIdChallengeExp.Has(g.ctx, collections.Join3(int64(99), user, "phoenix.social"))
	require.True(t, has)
	n, _ := g.keeper.PendingIdChallengeCount.Get(g.ctx, "phoenix.social")
	require.Equal(t, uint64(1), n)
	st, err := g.keeper.PeerRemovalQueue.Get(g.ctx, "aurora-gone")
	require.NoError(t, err)
	require.True(t, st.ContentDone)

	_, broken := keeper.BindingsByOperatorIndexInvariant(g.keeper)(g.sdkCtx())
	require.False(t, broken)
	_, broken = keeper.OrphanBindingsInvariant(g.keeper)(g.sdkCtx())
	require.False(t, broken)
}

// FED-04: an UNDERFUNDED operator's new binding starts suspended; an
// UNBONDING operator cannot bind at all, nor submit content.
func TestRegisterBridgeRespectsOperatorStatus(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.social")
	registerTestPeer(t, f, ms, "aurora.social")
	sk := wireServiceKeeper(t, f)
	opAddr := testAddr(t, f, "uf-op")
	status := servicetypes.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
	sk.GetOperatorFn = func(_ context.Context, _ []byte, st string) (servicetypes.Operator, bool) {
		return servicetypes.Operator{Address: opAddr, ServiceType: st, Controller: testOpsCommPolicyAddress, Status: status}, true
	}

	_, err := ms.RegisterBridge(f.ctx, &types.MsgRegisterBridge{Operator: opAddr, PeerId: "phoenix.social", Protocol: "activitypub", Endpoint: "https://e", StakeAmount: math.ZeroInt()})
	require.NoError(t, err)
	b, _ := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(opAddr, "phoenix.social"))
	require.True(t, b.Suspended)

	status = servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING
	_, err = ms.RegisterBridge(f.ctx, &types.MsgRegisterBridge{Operator: opAddr, PeerId: "aurora.social", Protocol: "activitypub", Endpoint: "https://e", StakeAmount: math.ZeroInt()})
	require.ErrorIs(t, err, types.ErrBindingNotActive)

	// An unsuspended binding of an UNBONDING operator cannot submit.
	b.Suspended = false
	require.NoError(t, f.keeper.BridgeBindings.Set(f.ctx, collections.Join(opAddr, "phoenix.social"), b))
	h := sha256.Sum256([]byte("x"))
	_, err = ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		License: "CC0-1.0", Operator: opAddr, PeerId: "phoenix.social", ContentType: "blog_post",
		CreatorIdentity: "@a@phoenix.social", ContentHash: h[:],
	})
	require.ErrorIs(t, err, types.ErrBindingNotActive)
}

// FED-12 / FED-13 / REP-R-07: self-dealing bans are alias-aware, and the
// arbiter path refuses suspended bindings and dispute parties.
func TestSelfDealingBansAliasAware(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.social")
	op := registerTestBridge(t, f, ms, "phoenix.social", "sd-op")
	opNew := testAddr(t, f, "sd-op-new")
	f.repKeeper.aliases = map[string]string{op: opNew}

	h := sha256.Sum256([]byte("self dealing"))
	contentID := submitTestContent(t, f, ms, op, "phoenix.social", h[:])

	// The operator's migrated key is still the operator.
	_ = bondTestVerifier(t, f, ms, "sd-op-new")
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{Creator: opNew, ContentId: contentID, ContentHash: h[:]})
	require.ErrorIs(t, err, types.ErrSelfVerification)

	verifier := bondTestVerifier(t, f, ms, "sd-verifier")
	_, err = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{Creator: verifier, ContentId: contentID, ContentHash: h[:]})
	require.NoError(t, err)
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{Creator: opNew, ContentId: contentID})
	require.ErrorIs(t, err, types.ErrSelfChallenge)

	challenger := registerTestBridge(t, f, ms, "phoenix.social", "sd-chal")
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{Creator: challenger, ContentId: contentID})
	require.NoError(t, err)

	// The challenger holds a binding for this peer but is a party.
	_, err = ms.SubmitArbiterHash(f.ctx, &types.MsgSubmitArbiterHash{Creator: challenger, ContentId: contentID, ContentHash: h[:]})
	require.ErrorIs(t, err, types.ErrSelfArbiter)

	// A suspended binding has no stake behind its vote.
	arb := registerTestBridge(t, f, ms, "phoenix.social", "sd-arb")
	b, _ := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(arb, "phoenix.social"))
	b.Suspended = true
	require.NoError(t, f.keeper.BridgeBindings.Set(f.ctx, collections.Join(arb, "phoenix.social"), b))
	_, err = ms.SubmitArbiterHash(f.ctx, &types.MsgSubmitArbiterHash{Creator: arb, ContentId: contentID, ContentHash: h[:]})
	require.ErrorIs(t, err, types.ErrBridgeNotActive)
}

// FED-17: the escalated-challenge verdict and peer removal need the
// committee POLICY, not one member.
func TestPolicyOnlyActions(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	member := testAddr(t, f, "ops-member")
	policy := testAddr(t, f, "ops-policy")
	f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, _, _ string) bool {
		return addr == policy
	}
	registerTestPeer(t, f, ms, "phoenix.social")

	_, err := ms.ResolveEscalatedChallenge(f.ctx, &types.MsgResolveEscalatedChallenge{
		Authority: member, ContentId: 1, Verdict: types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD,
	})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = ms.RemovePeer(f.ctx, &types.MsgRemovePeer{Authority: member, PeerId: "phoenix.social"})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: member})
	require.ErrorIs(t, err, types.ErrNotAuthorized)

	_, err = ms.RemovePeer(f.ctx, &types.MsgRemovePeer{Authority: policy, PeerId: "phoenix.social"})
	require.NoError(t, err)
}

// FED-18: moderation cannot manufacture VERIFIED content or touch an open
// dispute; rejecting / restoring needs the policy, hiding does not.
func TestModerationLimits(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	member := testAddr(t, f, "ops-member")
	policy := testAddr(t, f, "ops-policy")
	f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, _, _ string) bool {
		return addr == policy
	}
	registerTestPeer(t, f, ms, "phoenix.social")
	op := registerTestBridge(t, f, ms, "phoenix.social", "mod-op")
	h := sha256.Sum256([]byte("moderated"))
	contentID := submitTestContent(t, f, ms, op, "phoenix.social", h[:])

	_, err := ms.ModerateContent(f.ctx, &types.MsgModerateContent{Authority: policy, ContentId: contentID,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED})
	require.ErrorIs(t, err, types.ErrInvalidParamValue)

	_, err = ms.ModerateContent(f.ctx, &types.MsgModerateContent{Authority: member, ContentId: contentID,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN})
	require.NoError(t, err)
	_, err = ms.ModerateContent(f.ctx, &types.MsgModerateContent{Authority: member, ContentId: contentID,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE})
	require.ErrorIs(t, err, types.ErrNotAuthorized)

	// Restoring never-verified bridged content goes back to
	// PENDING_VERIFICATION, not ACTIVE / VERIFIED.
	_, err = ms.ModerateContent(f.ctx, &types.MsgModerateContent{Authority: policy, ContentId: contentID,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE})
	require.NoError(t, err)
	c, _ := f.keeper.Content.Get(f.ctx, contentID)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION, c.Status)

	// Mid-dispute: refused.
	verifier := bondTestVerifier(t, f, ms, "mod-verifier")
	_, err = ms.VerifyContent(f.ctx, &types.MsgVerifyContent{Creator: verifier, ContentId: contentID, ContentHash: h[:]})
	require.NoError(t, err)
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{Creator: testAddr(t, f, "mod-chal"), ContentId: contentID})
	require.NoError(t, err)
	_, err = ms.ModerateContent(f.ctx, &types.MsgModerateContent{Authority: member, ContentId: contentID,
		NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN})
	require.ErrorIs(t, err, types.ErrDisputeInProgress)
}

// FED-19: a challenge whose arbiter window closes without quorum disburses
// the escrowed fee (half refund, half burn) instead of stranding it.
func TestNoQuorumDisbursesChallengeFee(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.social")
	op := registerTestBridge(t, f, ms, "phoenix.social", "nq-op")
	h := sha256.Sum256([]byte("no quorum"))
	contentID := submitTestContent(t, f, ms, op, "phoenix.social", h[:])
	verifier := bondTestVerifier(t, f, ms, "nq-verifier")
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{Creator: verifier, ContentId: contentID, ContentHash: h[:]})
	require.NoError(t, err)
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{Creator: testAddr(t, f, "nq-chal"), ContentId: contentID})
	require.NoError(t, err)
	rec, _ := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.True(t, rec.EscrowedChallengeFee.IsPositive())

	params, _ := f.keeper.Params.Get(f.ctx)
	later := f.sdkCtx().WithBlockTime(f.sdkCtx().BlockTime().Add(params.ArbiterResolutionWindow + time.Second))
	require.NoError(t, f.keeper.EndBlocker(later))

	rec, _ = f.keeper.VerificationRecords.Get(later, contentID)
	require.True(t, rec.EscrowedChallengeFee.IsZero(), "fee must be disbursed, not stranded")
	c, _ := f.keeper.Content.Get(later, contentID)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, c.Status)
}

// FED-103: content TTL-pruned mid-dispute must not strand the escrowed fee
// on the no-quorum path.
func TestNoQuorumDisbursesFeeOfPrunedContent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.social")
	op := registerTestBridge(t, f, ms, "phoenix.social", "nqp-op")
	h := sha256.Sum256([]byte("no quorum pruned"))
	contentID := submitTestContent(t, f, ms, op, "phoenix.social", h[:])
	verifier := bondTestVerifier(t, f, ms, "nqp-verifier")
	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{Creator: verifier, ContentId: contentID, ContentHash: h[:]})
	require.NoError(t, err)
	_, err = ms.ChallengeVerification(f.ctx, &types.MsgChallengeVerification{Creator: testAddr(t, f, "nqp-chal"), ContentId: contentID})
	require.NoError(t, err)
	rec, _ := f.keeper.VerificationRecords.Get(f.ctx, contentID)
	require.True(t, rec.EscrowedChallengeFee.IsPositive())

	// the content record goes (TTL prune) while the dispute is open
	require.NoError(t, f.keeper.Content.Remove(f.ctx, contentID))

	params, _ := f.keeper.Params.Get(f.ctx)
	later := f.sdkCtx().WithBlockTime(f.sdkCtx().BlockTime().Add(params.ArbiterResolutionWindow + time.Second))
	require.NoError(t, f.keeper.EndBlocker(later))

	rec, err = f.keeper.VerificationRecords.Get(later, contentID)
	require.NoError(t, err)
	require.True(t, rec.EscrowedChallengeFee.IsZero(), "fee of pruned content must be disbursed, not stranded")
	require.True(t, rec.CommitmentReleased)
}

// FED-20: the operational path validates the merged params against the
// spec range table.
func TestOperationalParamsValidated(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	p, _ := f.keeper.Params.Get(f.ctx)
	op := types.FederationOperationalParams{
		MaxInboundPerBlock: p.MaxInboundPerBlock, MaxOutboundPerBlock: p.MaxOutboundPerBlock,
		MaxContentBodySize: p.MaxContentBodySize, MaxContentUriSize: p.MaxContentUriSize,
		MaxProtocolMetadataSize: 0, ContentTtl: p.ContentTtl, AttestationTtl: p.AttestationTtl,
		GlobalMaxTrustCredit: p.GlobalMaxTrustCredit, BridgeInactivityThreshold: p.BridgeInactivityThreshold,
		MaxPrunePerBlock: p.MaxPrunePerBlock,
	}
	_, err := ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.authority, OperationalParams: op})
	require.NoError(t, err, "metadata size 0 is allowed")

	bad := op
	bad.MaxPrunePerBlock = 1 << 40
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.authority, OperationalParams: bad})
	require.ErrorIs(t, err, types.ErrInvalidParamValue)
	bad = op
	bad.MaxContentBodySize = 1 << 40
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.authority, OperationalParams: bad})
	require.ErrorIs(t, err, types.ErrInvalidParamValue)
	bad = op
	bad.GlobalMaxTrustCredit = 9
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.authority, OperationalParams: bad})
	require.ErrorIs(t, err, types.ErrInvalidParamValue)
}

// FED-22: controller_group is validated and stored.
func TestRegisterPeerStoresControllerGroup(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	group := testAddr(t, f, "controller-group")
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "phoenix.social", Type: types.PeerType_PEER_TYPE_ACTIVITYPUB, ControllerGroup: group,
	})
	require.NoError(t, err)
	peer, _ := f.keeper.Peers.Get(f.ctx, "phoenix.social")
	require.Equal(t, group, peer.ControllerGroup)

	f.commonsKeeper.IsGroupPolicyAddressFn = func(context.Context, string) bool { return false }
	_, err = ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "aurora.social", Type: types.PeerType_PEER_TYPE_ACTIVITYPUB, ControllerGroup: group,
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

// FED-23: a stale unverified-expiry entry from an unlinked link never
// prunes a re-created link early.
func TestRelinkNotPrunedByStaleExpiry(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.social")
	user := testAddr(t, f, "relinker")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{Creator: user, PeerId: "phoenix.social", RemoteIdentity: "@typo@phoenix.social"})
	require.NoError(t, err)
	first, _ := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(user, "phoenix.social"))
	_, err = ms.UnlinkIdentity(f.ctx, &types.MsgUnlinkIdentity{Creator: user, PeerId: "phoenix.social"})
	require.NoError(t, err)
	// Simulate an entry left by an older binary (unlink used to leave it).
	require.NoError(t, f.keeper.UnverifiedLinkExp.Set(f.ctx, collections.Join3(first.UnverifiedExpiresAt, user, "phoenix.social")))

	later := f.sdkCtx().WithBlockTime(f.sdkCtx().BlockTime().Add(time.Minute))
	_, err = ms.LinkIdentity(later, &types.MsgLinkIdentity{Creator: user, PeerId: "phoenix.social", RemoteIdentity: "@right@phoenix.social"})
	require.NoError(t, err)

	atOldExpiry := f.sdkCtx().WithBlockTime(time.Unix(first.UnverifiedExpiresAt, 0))
	require.NoError(t, f.keeper.EndBlocker(atOldExpiry))
	link, err := f.keeper.IdentityLinks.Get(atOldExpiry, collections.Join(user, "phoenix.social"))
	require.NoError(t, err, "re-created link must survive the stale entry")
	require.Equal(t, "@right@phoenix.social", link.RemoteIdentity)
}

// FED-11: Phase 9 bridge cleanup removes the BindingsByOperator entry too.
func TestPeerRemovalPrunesBindingsByOperator(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.social")
	op := testAddr(t, f, "orphan-op")
	seedBinding(t, f, op, "phoenix.social", types.ServiceTypeFederationBridgeActivityPub)
	queuePeerRemoval(t, f, f.sdkCtx(), "phoenix.social")
	runEndBlockerUntilPeerGone(t, f, f.sdkCtx(), "phoenix.social")
	has, _ := f.keeper.BindingsByOperator.Has(f.ctx, collections.Join3(types.ServiceTypeFederationBridgeActivityPub, op, "phoenix.social"))
	require.False(t, has)
}

// FED-15: the overflow burn runs only on a reward-epoch boundary.
func TestOverflowBurnOnlyAtEpoch(t *testing.T) {
	f := initFixture(t)
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxOperatorRewardPool = math.NewInt(100)
	params.OperatorRewardPoolOverflowBurnRatio = math.LegacyOneDec()
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	pool := keeper.OperatorRewardPoolAddress().String()
	f.bankKeeper.balances[pool] = sdk.NewCoins(sdk.NewInt64Coin("uspark", 1000))

	off := f.sdkCtx().WithBlockHeight(int64(params.OperatorRewardEpochBlocks) + 1)
	require.NoError(t, f.keeper.EndBlocker(off))
	require.Equal(t, int64(1000), f.bankKeeper.balances[pool].AmountOf("uspark").Int64())

	on := f.sdkCtx().WithBlockHeight(int64(params.OperatorRewardEpochBlocks) * 2)
	require.NoError(t, f.keeper.EndBlocker(on))
	require.Equal(t, int64(100), f.bankKeeper.balances[pool].AmountOf("uspark").Int64())
}

// FED-24(c): hooks run in a cache context. A completed hook commits; an
// out-of-gas inside the hook is re-panicked so the enclosing tx fails
// instead of committing a half-applied prune.
func TestHookIsolation(t *testing.T) {
	f := initFixture(t)
	op := testAddr(t, f, "hook-op")
	seedBinding(t, f, op, "phoenix.social", types.ServiceTypeFederationBridgeActivityPub)
	seedBinding(t, f, op, "aurora.social", types.ServiceTypeFederationBridgeActivityPub)
	hooks := keeper.NewFederationServiceHooks(f.keeper)
	opBytes, _ := f.addressCodec.StringToBytes(op)

	hooks.AfterOperatorUnderfunded(f.ctx, sdk.AccAddress(opBytes), types.ServiceTypeFederationBridgeActivityPub)
	for _, p := range []string{"phoenix.social", "aurora.social"} {
		b, _ := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(op, p))
		require.True(t, b.Suspended)
	}

	starved := f.sdkCtx().WithGasMeter(storetypes.NewGasMeter(10))
	require.Panics(t, func() {
		hooks.AfterOperatorDissolved(starved, sdk.AccAddress(opBytes), types.ServiceTypeFederationBridgeActivityPub)
	})
	for _, p := range []string{"phoenix.social", "aurora.social"} {
		has, _ := f.keeper.BridgeBindings.Has(f.ctx, collections.Join(op, p))
		require.True(t, has, "an aborted hook must not leave a partial prune")
	}
}
