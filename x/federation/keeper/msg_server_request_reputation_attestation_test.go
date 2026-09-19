package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestRequestReputationAttestation(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestIBCPeer(t, f, ms, "rep-peer")

	// Set to ACTIVE and enable reputation
	peer, _ := f.keeper.Peers.Get(f.ctx, "rep-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "rep-peer", peer))
	require.NoError(t, f.keeper.PeerPolicies.Set(f.ctx, "rep-peer", types.PeerPolicy{
		PeerId: "rep-peer", AcceptReputationAttestations: true,
	}))

	userStr := testAddr(t, f, "rep-user")

	// A request now requires a VERIFIED identity link from the requester to
	// the address being asked about: the attestation is keyed
	// (local_address, peer_id), so without the link two requests about
	// different remote addresses on one peer would overwrite each other.
	seedVerifiedLink(t, f, userStr, "rep-peer", "remote-addr")

	_, err := ms.RequestReputationAttestation(f.ctx, &types.MsgRequestReputationAttestation{
		Creator: userStr, PeerId: "rep-peer", RemoteAddress: "remote-addr",
	})
	require.NoError(t, err)
}

// seedVerifiedLink writes a VERIFIED IdentityLink directly, bypassing the
// two-phase IBC handshake the real flow uses.
func seedVerifiedLink(t *testing.T, f *fixture, local, peerID, remote string) {
	t.Helper()
	require.NoError(t, f.keeper.IdentityLinks.Set(f.ctx,
		collections.Join(local, peerID),
		types.IdentityLink{
			LocalAddress:   local,
			PeerId:         peerID,
			RemoteIdentity: remote,
			Status:         types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED,
		}))
}

// The link gate: no link, an unverified link, and a link to a different
// remote identity must all be refused.
func TestRequestReputationAttestationRequiresVerifiedLink(t *testing.T) {
	setup := func(t *testing.T) (*fixture, types.MsgServer, string) {
		t.Helper()
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		registerTestIBCPeer(t, f, ms, "rep-peer")
		peer, _ := f.keeper.Peers.Get(f.ctx, "rep-peer")
		peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
		require.NoError(t, f.keeper.Peers.Set(f.ctx, "rep-peer", peer))
		require.NoError(t, f.keeper.PeerPolicies.Set(f.ctx, "rep-peer", types.PeerPolicy{
			PeerId: "rep-peer", AcceptReputationAttestations: true,
		}))
		return f, ms, testAddr(t, f, "linkless-user")
	}

	t.Run("no link at all", func(t *testing.T) {
		f, ms, user := setup(t)
		_ = f
		_, err := ms.RequestReputationAttestation(f.ctx, &types.MsgRequestReputationAttestation{
			Creator: user, PeerId: "rep-peer", RemoteAddress: "remote-addr",
		})
		require.ErrorIs(t, err, types.ErrIdentityLinkNotFound)
	})

	t.Run("link exists but is unverified", func(t *testing.T) {
		f, ms, user := setup(t)
		require.NoError(t, f.keeper.IdentityLinks.Set(f.ctx,
			collections.Join(user, "rep-peer"),
			types.IdentityLink{
				LocalAddress:   user,
				PeerId:         "rep-peer",
				RemoteIdentity: "remote-addr",
				Status:         types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED,
			}))
		_, err := ms.RequestReputationAttestation(f.ctx, &types.MsgRequestReputationAttestation{
			Creator: user, PeerId: "rep-peer", RemoteAddress: "remote-addr",
		})
		require.ErrorIs(t, err, types.ErrIdentityLinkNotFound)
	})

	t.Run("verified link names a different remote identity", func(t *testing.T) {
		f, ms, user := setup(t)
		seedVerifiedLink(t, f, user, "rep-peer", "someone-else")
		_, err := ms.RequestReputationAttestation(f.ctx, &types.MsgRequestReputationAttestation{
			Creator: user, PeerId: "rep-peer", RemoteAddress: "remote-addr",
		})
		require.ErrorIs(t, err, types.ErrIdentityLinkNotFound)
	})
}

func TestRequestReputationAttestationWrongPeerType(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "ap-rep-peer") // ActivityPub

	peer, _ := f.keeper.Peers.Get(f.ctx, "ap-rep-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "ap-rep-peer", peer))

	userStr := testAddr(t, f, "rep-user2")
	_, err := ms.RequestReputationAttestation(f.ctx, &types.MsgRequestReputationAttestation{
		Creator: userStr, PeerId: "ap-rep-peer", RemoteAddress: "remote-addr",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Spark Dream")
}
