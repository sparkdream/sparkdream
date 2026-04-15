package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestUpdateBridge(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "update-peer")
	opStr := registerTestBridge(t, f, ms, "update-peer", "update-op")

	_, err := ms.UpdateBridge(f.ctx, &types.MsgUpdateBridge{
		Authority: f.authority, Operator: opStr, PeerId: "update-peer",
		Endpoint: "https://new-endpoint.example.com",
	})
	require.NoError(t, err)

	bridge, _ := f.keeper.BridgeOperators.Get(f.ctx, collections.Join(opStr, "update-peer"))
	require.Equal(t, "https://new-endpoint.example.com", bridge.Endpoint)
}
