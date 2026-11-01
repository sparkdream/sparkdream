package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestUpdatePeerInfo(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "info-peer")
	before, err := f.keeper.Peers.Get(f.ctx, "info-peer")
	require.NoError(t, err)

	_, err = ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{
		Authority: f.authority, PeerId: "info-peer",
		DisplayName: "Spark Dream Mastodon", Metadata: `{"software":"mastodon"}`,
	})
	require.NoError(t, err)

	after, err := f.keeper.Peers.Get(f.ctx, "info-peer")
	require.NoError(t, err)
	require.Equal(t, "Spark Dream Mastodon", after.DisplayName)
	require.Equal(t, `{"software":"mastodon"}`, after.Metadata)
	// Nothing else moves.
	after.DisplayName, after.Metadata = before.DisplayName, before.Metadata
	require.Equal(t, before, after)
}

func TestUpdatePeerInfoRejectsUnauthorized(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "info-peer")
	f.keeper.SetCommonsKeeper(&mockCommonsKeeper{
		IsCouncilAuthorizedFn: func(context.Context, string, string, string) bool { return false },
	})

	_, err := ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{
		Authority: testAddr(t, f, "random"), PeerId: "info-peer", DisplayName: "x",
	})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
}

func TestUpdatePeerInfoRefusesMissingAndRemovedPeers(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{
		Authority: f.authority, PeerId: "nonexistent", DisplayName: "x",
	})
	require.ErrorIs(t, err, types.ErrPeerNotFound)

	registerTestPeer(t, f, ms, "removed-peer")
	peer, err := f.keeper.Peers.Get(f.ctx, "removed-peer")
	require.NoError(t, err)
	peer.Status = types.PeerStatus_PEER_STATUS_REMOVED
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "removed-peer", peer))

	_, err = ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{
		Authority: f.authority, PeerId: "removed-peer", DisplayName: "x",
	})
	require.ErrorIs(t, err, types.ErrPeerNotActive)
}
