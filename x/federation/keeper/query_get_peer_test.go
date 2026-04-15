package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestQueryGetPeer(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)

	registerTestPeer(t, f, ms, "query-peer")

	resp, err := qs.GetPeer(f.ctx, &types.QueryGetPeerRequest{PeerId: "query-peer"})
	require.NoError(t, err)
	require.Equal(t, "query-peer", resp.Peer.Id)

	_, err = qs.GetPeer(f.ctx, &types.QueryGetPeerRequest{PeerId: "nonexistent"})
	require.Error(t, err)
}
