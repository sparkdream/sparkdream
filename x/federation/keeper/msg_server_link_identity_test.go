package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestLinkIdentity(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "link-peer")

	peer, _ := f.keeper.Peers.Get(f.ctx, "link-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "link-peer", peer))

	userStr := testAddr(t, f, "link-user")

	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: userStr, PeerId: "link-peer", RemoteIdentity: "@alice@mastodon.social",
	})
	require.NoError(t, err)

	// Verify link stored
	link, err := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(userStr, "link-peer"))
	require.NoError(t, err)
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED, link.Status)

	// Reverse index
	resolved, err := f.keeper.IdentityLinksByRemote.Get(f.ctx, collections.Join("link-peer", "@alice@mastodon.social"))
	require.NoError(t, err)
	require.Equal(t, userStr, resolved)

	// Count incremented
	count, _ := f.keeper.IdentityLinkCount.Get(f.ctx, userStr)
	require.Equal(t, uint32(1), count)
}

func TestLinkIdentityDuplicate(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "dup-link-peer")
	peer, _ := f.keeper.Peers.Get(f.ctx, "dup-link-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "dup-link-peer", peer))

	userStr := testAddr(t, f, "dup-user")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: userStr, PeerId: "dup-link-peer", RemoteIdentity: "@bob@example.com",
	})
	require.NoError(t, err)

	_, err = ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: userStr, PeerId: "dup-link-peer", RemoteIdentity: "@bob2@example.com",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestLinkIdentityRemoteClaimed(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "claimed-peer")
	peer, _ := f.keeper.Peers.Get(f.ctx, "claimed-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "claimed-peer", peer))

	user1 := testAddr(t, f, "claim-user1")
	user2 := testAddr(t, f, "claim-user2")

	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: user1, PeerId: "claimed-peer", RemoteIdentity: "@shared@example.com",
	})
	require.NoError(t, err)

	_, err = ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: user2, PeerId: "claimed-peer", RemoteIdentity: "@shared@example.com",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already claims")
}
