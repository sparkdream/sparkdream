package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestQueryListPeers(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)

	registerTestPeer(t, f, ms, "list-peer-1")
	registerTestPeer(t, f, ms, "list-peer-2")

	resp, err := qs.ListPeers(f.ctx, &types.QueryListPeersRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Peers, 2)
}
