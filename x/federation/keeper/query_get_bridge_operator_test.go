package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestQueryGetBridgeOperator(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)

	registerTestPeer(t, f, ms, "bridge-q-peer")
	opStr := registerTestBridge(t, f, ms, "bridge-q-peer", "bridge-q-op")

	resp, err := qs.GetBridgeBinding(f.ctx, &types.QueryGetBridgeBindingRequest{
		Address: opStr, PeerId: "bridge-q-peer",
	})
	require.NoError(t, err)
	require.Equal(t, opStr, resp.BridgeBinding.Address)

	_, err = qs.GetBridgeBinding(f.ctx, &types.QueryGetBridgeBindingRequest{
		Address: "nonexistent", PeerId: "bridge-q-peer",
	})
	require.Error(t, err)
}
