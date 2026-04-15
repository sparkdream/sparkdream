package keeper_test

import (
	"testing"

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
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "resolve-peer", peer))

	userStr := testAddr(t, f, "resolve-user")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: userStr, PeerId: "resolve-peer", RemoteIdentity: "@bob@example.com",
	})
	require.NoError(t, err)

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
