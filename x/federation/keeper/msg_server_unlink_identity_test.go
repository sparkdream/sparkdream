package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestUnlinkIdentity(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "unlink-peer")
	peer, _ := f.keeper.Peers.Get(f.ctx, "unlink-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "unlink-peer", peer))

	userStr := testAddr(t, f, "unlink-user")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: userStr, PeerId: "unlink-peer", RemoteIdentity: "@test@example.com",
	})
	require.NoError(t, err)

	_, err = ms.UnlinkIdentity(f.ctx, &types.MsgUnlinkIdentity{
		Creator: userStr, PeerId: "unlink-peer",
	})
	require.NoError(t, err)

	// Link removed
	_, err = f.keeper.IdentityLinks.Get(f.ctx, collections.Join(userStr, "unlink-peer"))
	require.Error(t, err)

	// Reverse removed
	_, err = f.keeper.IdentityLinksByRemote.Get(f.ctx, collections.Join("unlink-peer", "@test@example.com"))
	require.Error(t, err)

	// Count decremented
	count, _ := f.keeper.IdentityLinkCount.Get(f.ctx, userStr)
	require.Equal(t, uint32(0), count)
}

func TestUnlinkIdentityNotFound(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	userStr := testAddr(t, f, "no-link-user")

	_, err := ms.UnlinkIdentity(f.ctx, &types.MsgUnlinkIdentity{
		Creator: userStr, PeerId: "nonexistent",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no link")
}
