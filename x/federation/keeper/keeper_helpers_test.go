package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestIsCouncilAuthorized(t *testing.T) {
	f := initFixture(t)

	// Authority (governance) is always authorized
	require.True(t, f.keeper.IsCouncilAuthorized(f.ctx, f.authority, "commons", ""))

	// Random address authorized (mock always returns true)
	addr := testAddr(t, f, "random-user")
	require.True(t, f.keeper.IsCouncilAuthorized(f.ctx, addr, "commons", "operations"))
}

func TestGetPeerRequireActive(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "active-test-peer")

	// PENDING peer should fail
	_, err := f.keeper.GetPeerRequireActive(f.ctx, "active-test-peer")
	require.Error(t, err)

	// Set to ACTIVE
	peer, _ := f.keeper.Peers.Get(f.ctx, "active-test-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "active-test-peer", peer))

	got, err := f.keeper.GetPeerRequireActive(f.ctx, "active-test-peer")
	require.NoError(t, err)
	require.Equal(t, "active-test-peer", got.Id)

	// Nonexistent fails
	_, err = f.keeper.GetPeerRequireActive(f.ctx, "nonexistent")
	require.Error(t, err)
}
