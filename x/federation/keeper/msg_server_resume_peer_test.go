package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestResumePeer(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "resume-peer")

	// Set to SUSPENDED
	peer, _ := f.keeper.Peers.Get(f.ctx, "resume-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_SUSPENDED
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "resume-peer", peer))

	// Resume
	_, err := ms.ResumePeer(f.ctx, &types.MsgResumePeer{
		Authority: f.authority, PeerId: "resume-peer",
	})
	require.NoError(t, err)
	peer, _ = f.keeper.Peers.Get(f.ctx, "resume-peer")
	require.Equal(t, types.PeerStatus_PEER_STATUS_ACTIVE, peer.Status)

	// Resume non-suspended fails
	_, err = ms.ResumePeer(f.ctx, &types.MsgResumePeer{
		Authority: f.authority, PeerId: "resume-peer",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not suspended or pending")
}
