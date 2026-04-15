package keeper_test

import (
	"testing"

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
	_, err := ms.RequestReputationAttestation(f.ctx, &types.MsgRequestReputationAttestation{
		Creator: userStr, PeerId: "rep-peer", RemoteAddress: "remote-addr",
	})
	require.NoError(t, err)
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
