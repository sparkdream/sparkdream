package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestRemovePeer(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "doomed-peer")

	// Remove
	_, err := ms.RemovePeer(f.ctx, &types.MsgRemovePeer{
		Authority: f.authority, PeerId: "doomed-peer", Reason: "no longer needed",
	})
	require.NoError(t, err)

	peer, _ := f.keeper.Peers.Get(f.ctx, "doomed-peer")
	require.Equal(t, types.PeerStatus_PEER_STATUS_REMOVED, peer.Status)

	// In removal queue
	_, err = f.keeper.PeerRemovalQueue.Get(f.ctx, "doomed-peer")
	require.NoError(t, err)

	// Re-register blocked during cleanup
	_, err = ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "doomed-peer", DisplayName: "Reborn",
		Type: types.PeerType_PEER_TYPE_ACTIVITYPUB,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cleanup")

	// After cleanup completes, re-register works
	require.NoError(t, f.keeper.PeerRemovalQueue.Remove(f.ctx, "doomed-peer"))
	_, err = ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "doomed-peer", DisplayName: "Reborn",
		Type: types.PeerType_PEER_TYPE_ACTIVITYPUB,
	})
	require.NoError(t, err)
}

func TestRemovePeerNotFound(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.RemovePeer(f.ctx, &types.MsgRemovePeer{
		Authority: f.authority, PeerId: "nonexistent", Reason: "test",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}
