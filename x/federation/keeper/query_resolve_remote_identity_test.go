package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestQueryResolveRemoteIdentity(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)

	registerTestPeer(t, f, ms, "resolve-peer")
	peer, _ := f.keeper.Peers.Get(f.ctx, "resolve-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.SetPeer(f.ctx, peer))

	userStr := testAddr(t, f, "resolve-user")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: userStr, PeerId: "resolve-peer", RemoteIdentity: "@bob@example.com",
	})
	require.NoError(t, err)

	// FED-28: an unverified (self-asserted) claim does not resolve.
	_, err = qs.ResolveRemoteIdentity(f.ctx, &types.QueryResolveRemoteIdentityRequest{
		PeerId: "resolve-peer", RemoteIdentity: "@bob@example.com",
	})
	require.Error(t, err)

	// A verified link does.
	link, err := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(userStr, "resolve-peer"))
	require.NoError(t, err)
	link.Status = types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED
	require.NoError(t, f.keeper.IdentityLinks.Set(f.ctx, collections.Join(userStr, "resolve-peer"), link))
	require.NoError(t, f.keeper.IdentityLinksByRemote.Set(f.ctx, collections.Join("resolve-peer", "@bob@example.com"), userStr))
	resp, err := qs.ResolveRemoteIdentity(f.ctx, &types.QueryResolveRemoteIdentityRequest{
		PeerId: "resolve-peer", RemoteIdentity: "@bob@example.com",
	})
	require.NoError(t, err)
	require.Equal(t, userStr, resp.LocalAddress)

	_, err = qs.ResolveRemoteIdentity(f.ctx, &types.QueryResolveRemoteIdentityRequest{
		PeerId: "resolve-peer", RemoteIdentity: "@nobody@example.com",
	})
	require.Error(t, err)
}
