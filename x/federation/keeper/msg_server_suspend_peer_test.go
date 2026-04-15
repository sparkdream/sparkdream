package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestSuspendPeer(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "test-peer")

	// Set to ACTIVE manually
	peer, _ := f.keeper.Peers.Get(f.ctx, "test-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "test-peer", peer))

	// Suspend
	_, err := ms.SuspendPeer(f.ctx, &types.MsgSuspendPeer{
		Authority: f.authority, PeerId: "test-peer", Reason: "maintenance",
	})
	require.NoError(t, err)
	peer, _ = f.keeper.Peers.Get(f.ctx, "test-peer")
	require.Equal(t, types.PeerStatus_PEER_STATUS_SUSPENDED, peer.Status)

	// Double-suspend fails
	_, err = ms.SuspendPeer(f.ctx, &types.MsgSuspendPeer{
		Authority: f.authority, PeerId: "test-peer", Reason: "again",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
}

func TestSuspendPeerNotFound(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.SuspendPeer(f.ctx, &types.MsgSuspendPeer{
		Authority: f.authority, PeerId: "nonexistent", Reason: "test",
	})
	require.Error(t, err)
}
