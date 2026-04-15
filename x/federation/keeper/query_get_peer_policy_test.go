package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestQueryGetPeerPolicy(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)

	registerTestPeer(t, f, ms, "policy-q-peer")

	resp, err := qs.GetPeerPolicy(f.ctx, &types.QueryGetPeerPolicyRequest{PeerId: "policy-q-peer"})
	require.NoError(t, err)
	require.Equal(t, "policy-q-peer", resp.Policy.PeerId)
}
