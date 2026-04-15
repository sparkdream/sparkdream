package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestFederateContent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestIBCPeer(t, f, ms, "ibc-peer")

	// Set outbound policy
	_, err := ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority, PeerId: "ibc-peer",
		Policy: types.PeerPolicy{OutboundContentTypes: []string{"blog_post"}},
	})
	require.NoError(t, err)

	// Manually set to ACTIVE
	peer, _ := f.keeper.Peers.Get(f.ctx, "ibc-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "ibc-peer", peer))

	creatorStr := testAddr(t, f, "content-creator")
	_, err = ms.FederateContent(f.ctx, &types.MsgFederateContent{
		Creator: creatorStr, PeerId: "ibc-peer", ContentType: "blog_post",
		LocalContentId: "123", Title: "Test", Body: "Content",
	})
	require.NoError(t, err)
}

func TestFederateContentWrongPeerType(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "ap-peer") // ActivityPub, not IBC

	peer, _ := f.keeper.Peers.Get(f.ctx, "ap-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "ap-peer", peer))

	creatorStr := testAddr(t, f, "wrong-creator")
	_, err := ms.FederateContent(f.ctx, &types.MsgFederateContent{
		Creator: creatorStr, PeerId: "ap-peer", ContentType: "blog_post",
		LocalContentId: "456",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "IBC peers")
}
