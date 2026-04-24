package simulation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// ─── find helpers ────────────────────────────────────────────────────────────

// findPeerByStatus returns a random peer with the given status, or nil.
func findPeerByStatus(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.PeerStatus) (*types.Peer, error) {
	var peers []types.Peer
	err := k.Peers.Walk(ctx, nil, func(id string, peer types.Peer) (bool, error) {
		if peer.Status == status {
			peers = append(peers, peer)
		}
		return false, nil
	})
	if err != nil || len(peers) == 0 {
		return nil, err
	}
	return &peers[r.Intn(len(peers))], nil
}

// findActiveBridge returns a random active bridge operator, or nil.
func findActiveBridge(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.BridgeOperator, error) {
	var bridges []types.BridgeOperator
	err := k.BridgeOperators.Walk(ctx, nil, func(_ collections.Pair[string, string], bridge types.BridgeOperator) (bool, error) {
		if bridge.Status == types.BridgeStatus_BRIDGE_STATUS_ACTIVE {
			bridges = append(bridges, bridge)
		}
		return false, nil
	})
	if err != nil || len(bridges) == 0 {
		return nil, err
	}
	return &bridges[r.Intn(len(bridges))], nil
}

// findRevokedBridge returns a random revoked bridge operator, or nil.
func findRevokedBridge(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.BridgeOperator, error) {
	var bridges []types.BridgeOperator
	err := k.BridgeOperators.Walk(ctx, nil, func(_ collections.Pair[string, string], bridge types.BridgeOperator) (bool, error) {
		if bridge.Status == types.BridgeStatus_BRIDGE_STATUS_REVOKED {
			bridges = append(bridges, bridge)
		}
		return false, nil
	})
	if err != nil || len(bridges) == 0 {
		return nil, err
	}
	return &bridges[r.Intn(len(bridges))], nil
}

// findContentByStatus returns a random content item with the given status, or nil.
func findContentByStatus(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.FederatedContentStatus) (*types.FederatedContent, uint64, error) {
	type entry struct {
		id      uint64
		content types.FederatedContent
	}
	var entries []entry
	err := k.Content.Walk(ctx, nil, func(id uint64, content types.FederatedContent) (bool, error) {
		if content.Status == status {
			entries = append(entries, entry{id, content})
		}
		return false, nil
	})
	if err != nil || len(entries) == 0 {
		return nil, 0, err
	}
	e := entries[r.Intn(len(entries))]
	return &e.content, e.id, nil
}

// Note: findActiveVerifier was removed along with the local FederationVerifier
// collection in Phase 4 of the bonded-role generalization. Simulations that
// need a bonded verifier should assume it exists (the underlying tx will
// error cleanly if not), matching how other simulations handle rep-owned state.

// findIdentityLink returns a random identity link, or nil.
func findIdentityLink(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.IdentityLink, error) {
	var links []types.IdentityLink
	err := k.IdentityLinks.Walk(ctx, nil, func(_ collections.Pair[string, string], link types.IdentityLink) (bool, error) {
		links = append(links, link)
		return false, nil
	})
	if err != nil || len(links) == 0 {
		return nil, err
	}
	return &links[r.Intn(len(links))], nil
}

// findIdentityLinkByStatus returns a random identity link with the given status, or nil.
func findIdentityLinkByStatus(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.IdentityLinkStatus) (*types.IdentityLink, error) {
	var links []types.IdentityLink
	err := k.IdentityLinks.Walk(ctx, nil, func(_ collections.Pair[string, string], link types.IdentityLink) (bool, error) {
		if link.Status == status {
			links = append(links, link)
		}
		return false, nil
	})
	if err != nil || len(links) == 0 {
		return nil, err
	}
	return &links[r.Intn(len(links))], nil
}

// findVerificationRecord returns a random verification record with PENDING outcome, or nil.
func findVerificationRecord(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.VerificationRecord, error) {
	var records []types.VerificationRecord
	err := k.VerificationRecords.Walk(ctx, nil, func(_ uint64, rec types.VerificationRecord) (bool, error) {
		if rec.Outcome == types.VerificationOutcome_VERIFICATION_OUTCOME_PENDING {
			records = append(records, rec)
		}
		return false, nil
	})
	if err != nil || len(records) == 0 {
		return nil, err
	}
	return &records[r.Intn(len(records))], nil
}

// ─── get-or-create helpers ──────────────────────────────────────────────────

// getOrCreateActivePeer returns an existing active peer or creates one.
func getOrCreateActivePeer(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, registeredBy string) (types.Peer, error) {
	p, err := findPeerByStatus(r, ctx, k, types.PeerStatus_PEER_STATUS_ACTIVE)
	if err == nil && p != nil {
		return *p, nil
	}

	peerID := randomPeerID(r)
	peer := types.Peer{
		Id:           peerID,
		DisplayName:  "Sim Peer " + peerID[:8],
		Type:         randomPeerType(r),
		Status:       types.PeerStatus_PEER_STATUS_ACTIVE,
		IbcChannelId: fmt.Sprintf("channel-%d", r.Intn(100)),
		RegisteredAt: ctx.BlockTime().Unix(),
		LastActivity: ctx.BlockTime().Unix(),
		RegisteredBy: registeredBy,
		Metadata:     "simulation peer",
	}

	if err := k.Peers.Set(ctx, peerID, peer); err != nil {
		return types.Peer{}, err
	}

	// Create default policy
	policy := types.PeerPolicy{
		PeerId:                       peerID,
		OutboundContentTypes:         types.DefaultKnownContentTypes,
		InboundContentTypes:          types.DefaultKnownContentTypes,
		MinOutboundTrustLevel:        1,
		InboundRateLimitPerEpoch:     100,
		OutboundRateLimitPerEpoch:    100,
		AllowReputationQueries:       true,
		AcceptReputationAttestations: true,
		MaxTrustCredit:               1,
	}
	if err := k.PeerPolicies.Set(ctx, peerID, policy); err != nil {
		return types.Peer{}, err
	}

	return peer, nil
}

// getOrCreateSuspendedPeer returns an existing suspended peer or creates one.
func getOrCreateSuspendedPeer(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, registeredBy string) (types.Peer, error) {
	p, err := findPeerByStatus(r, ctx, k, types.PeerStatus_PEER_STATUS_SUSPENDED)
	if err == nil && p != nil {
		return *p, nil
	}

	// Create an active peer and suspend it
	peer, err := getOrCreateActivePeer(r, ctx, k, registeredBy)
	if err != nil {
		return types.Peer{}, err
	}
	peer.Status = types.PeerStatus_PEER_STATUS_SUSPENDED
	if err := k.Peers.Set(ctx, peer.Id, peer); err != nil {
		return types.Peer{}, err
	}
	return peer, nil
}

// getOrCreateActiveBridge returns an existing active bridge or creates one.
func getOrCreateActiveBridge(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, operator string) (types.BridgeOperator, error) {
	b, err := findActiveBridge(r, ctx, k)
	if err == nil && b != nil {
		return *b, nil
	}

	// Need an active peer first
	peer, err := getOrCreateActivePeer(r, ctx, k, operator)
	if err != nil {
		return types.BridgeOperator{}, err
	}

	bridge := types.BridgeOperator{
		Address:      operator,
		PeerId:       peer.Id,
		Protocol:     randomProtocol(r),
		Endpoint:     randomEndpoint(r),
		Stake:        sdk.NewCoin("uspark", math.NewInt(int64(r.Intn(9000)+1000)*1_000_000)),
		RegisteredAt: ctx.BlockTime().Unix(),
		Status:       types.BridgeStatus_BRIDGE_STATUS_ACTIVE,
	}

	if err := k.BridgeOperators.Set(ctx, collections.Join(operator, peer.Id), bridge); err != nil {
		return types.BridgeOperator{}, err
	}
	if err := k.BridgesByPeer.Set(ctx, collections.Join(peer.Id, operator)); err != nil {
		return types.BridgeOperator{}, err
	}

	return bridge, nil
}

// getOrCreatePendingContent returns existing pending content or creates one.
func getOrCreatePendingContent(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, submittedBy string) (types.FederatedContent, uint64, error) {
	c, id, err := findContentByStatus(r, ctx, k, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION)
	if err == nil && c != nil {
		return *c, id, nil
	}

	// Need an active peer
	peer, err := getOrCreateActivePeer(r, ctx, k, submittedBy)
	if err != nil {
		return types.FederatedContent{}, 0, err
	}

	contentID, err := k.ContentSeq.Next(ctx)
	if err != nil {
		return types.FederatedContent{}, 0, err
	}

	hash := randomContentHash(r)
	content := types.FederatedContent{
		Id:              contentID,
		PeerId:          peer.Id,
		RemoteContentId: fmt.Sprintf("remote-%d", r.Intn(10000)),
		ContentType:     randomContentType(r),
		CreatorIdentity: fmt.Sprintf("user@%s", peer.Id[:8]),
		CreatorName:     randomCreatorName(r),
		Title:           randomContentTitle(r),
		Body:            randomContentBody(r),
		RemoteCreatedAt: ctx.BlockTime().Unix() - int64(r.Intn(86400)),
		ReceivedAt:      ctx.BlockTime().Unix(),
		SubmittedBy:     submittedBy,
		Status:          types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION,
		ExpiresAt:       ctx.BlockTime().Unix() + int64(types.DefaultParams().ContentTtl.Seconds()),
		ContentHash:     hash,
	}

	if err := k.Content.Set(ctx, contentID, content); err != nil {
		return types.FederatedContent{}, 0, err
	}
	// Set indexes
	_ = k.ContentByPeer.Set(ctx, collections.Join(peer.Id, contentID))
	_ = k.ContentByType.Set(ctx, collections.Join(content.ContentType, contentID))
	_ = k.ContentByCreator.Set(ctx, collections.Join(content.CreatorIdentity, contentID))
	_ = k.ContentByHash.Set(ctx, hex.EncodeToString(hash), contentID)
	_ = k.ContentExpiration.Set(ctx, collections.Join(content.ExpiresAt, contentID))

	return content, contentID, nil
}

// getOrCreateVerifiedContent returns existing verified content or creates one.
func getOrCreateVerifiedContent(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, submittedBy string) (types.FederatedContent, uint64, error) {
	c, id, err := findContentByStatus(r, ctx, k, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED)
	if err == nil && c != nil {
		return *c, id, nil
	}

	// Create pending content and mark as verified
	content, contentID, err := getOrCreatePendingContent(r, ctx, k, submittedBy)
	if err != nil {
		return types.FederatedContent{}, 0, err
	}
	content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
	if err := k.Content.Set(ctx, contentID, content); err != nil {
		return types.FederatedContent{}, 0, err
	}
	return content, contentID, nil
}

// getOrCreateChallengedContent returns existing challenged content or creates one.
func getOrCreateChallengedContent(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, submittedBy string) (types.FederatedContent, uint64, error) {
	c, id, err := findContentByStatus(r, ctx, k, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED)
	if err == nil && c != nil {
		return *c, id, nil
	}

	// Create verified content and challenge it
	content, contentID, err := getOrCreateVerifiedContent(r, ctx, k, submittedBy)
	if err != nil {
		return types.FederatedContent{}, 0, err
	}
	content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED
	if err := k.Content.Set(ctx, contentID, content); err != nil {
		return types.FederatedContent{}, 0, err
	}
	return content, contentID, nil
}

// getOrCreateVerifier is a no-op stub under the Phase 4 bonded-role refactor:
// verifier bonding now lives on x/rep's BondedRole (ROLE_TYPE_FEDERATION_VERIFIER)
// and simulations cannot directly create bonds from within the federation sim.
// When the simulation invokes a downstream msg that requires a verifier, the
// msg handler will return ErrVerifierNotFound and the sim records a NoOp,
// which is acceptable for fuzz coverage.
func getOrCreateVerifier(_ *rand.Rand, _ sdk.Context, _ keeper.Keeper, addr string) (types.VerifierActivity, error) {
	return types.VerifierActivity{Address: addr}, nil
}

// getOrCreateIdentityLink returns an existing identity link or creates one.
func getOrCreateIdentityLink(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, localAddr string) (types.IdentityLink, error) {
	link, err := findIdentityLink(r, ctx, k)
	if err == nil && link != nil {
		return *link, nil
	}

	// Need an active peer
	peer, err := getOrCreateActivePeer(r, ctx, k, localAddr)
	if err != nil {
		return types.IdentityLink{}, err
	}

	newLink := types.IdentityLink{
		LocalAddress:   localAddr,
		PeerId:         peer.Id,
		RemoteIdentity: fmt.Sprintf("remote-user-%s", simtypes.RandStringOfLength(r, 8)),
		Status:         types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED,
		LinkedAt:       ctx.BlockTime().Unix(),
	}

	if err := k.IdentityLinks.Set(ctx, collections.Join(localAddr, peer.Id), newLink); err != nil {
		return types.IdentityLink{}, err
	}
	_ = k.IdentityLinksByRemote.Set(ctx, collections.Join(peer.Id, newLink.RemoteIdentity), localAddr)

	// Update link count
	count, _ := k.IdentityLinkCount.Get(ctx, localAddr)
	_ = k.IdentityLinkCount.Set(ctx, localAddr, count+1)

	return newLink, nil
}

// ─── utility helpers ────────────────────────────────────────────────────────

func randomPeerID(r *rand.Rand) string {
	return fmt.Sprintf("peer-%s", simtypes.RandStringOfLength(r, 12))
}

func randomPeerType(r *rand.Rand) types.PeerType {
	pts := []types.PeerType{
		types.PeerType_PEER_TYPE_SPARK_DREAM,
		types.PeerType_PEER_TYPE_ACTIVITYPUB,
		types.PeerType_PEER_TYPE_ATPROTO,
	}
	return pts[r.Intn(len(pts))]
}

func randomContentType(r *rand.Rand) string {
	return types.DefaultKnownContentTypes[r.Intn(len(types.DefaultKnownContentTypes))]
}

func randomProtocol(r *rand.Rand) string {
	protocols := []string{"activitypub", "atproto", "spark-ibc"}
	return protocols[r.Intn(len(protocols))]
}

func randomEndpoint(r *rand.Rand) string {
	endpoints := []string{
		"https://bridge.example.com/api",
		"https://relay.federation.io/v1",
		"https://node.sparkdream.net/bridge",
		"https://gateway.cosmos.network/fed",
	}
	return endpoints[r.Intn(len(endpoints))]
}

func randomContentTitle(r *rand.Rand) string {
	titles := []string{
		"Federated Post from Remote",
		"Cross-Chain Content Update",
		"Bridge Relay Article",
		"Remote Community Discussion",
		"Federation Test Content",
	}
	return titles[r.Intn(len(titles))]
}

func randomContentBody(r *rand.Rand) string {
	bodies := []string{
		"This is federated content from a remote peer.",
		"Cross-chain article discussing governance.",
		"Bridge-relayed post about community coordination.",
		"Remote content submitted via federation bridge.",
		"Simulation-generated federated content body.",
	}
	return bodies[r.Intn(len(bodies))]
}

func randomCreatorName(r *rand.Rand) string {
	names := []string{"phoenix", "aurora", "zenith", "nebula", "cascade", "ember"}
	return names[r.Intn(len(names))]
}

func randomReason(r *rand.Rand) string {
	reasons := []string{
		"Simulation test action",
		"Routine maintenance",
		"Policy violation detected",
		"Quality assurance check",
		"Operational requirement",
	}
	return reasons[r.Intn(len(reasons))]
}

func randomEvidence(r *rand.Rand) string {
	evidence := []string{
		"Content hash mismatch detected",
		"Remote source unavailable for verification",
		"Metadata inconsistency found",
		"Duplicate content from different source",
	}
	return evidence[r.Intn(len(evidence))]
}

func randomContentHash(r *rand.Rand) []byte {
	data := []byte(simtypes.RandStringOfLength(r, 32))
	hash := sha256.Sum256(data)
	return hash[:]
}

func getAccountForAddress(addr string, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == addr {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}
