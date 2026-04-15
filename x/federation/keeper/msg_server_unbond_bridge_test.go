package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestUnbondBridge(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "unbond-peer")
	opStr := registerTestBridge(t, f, ms, "unbond-peer", "unbond-op")

	_, err := ms.UnbondBridge(f.ctx, &types.MsgUnbondBridge{
		Operator: opStr, PeerId: "unbond-peer",
	})
	require.NoError(t, err)

	bridge, _ := f.keeper.BridgeOperators.Get(f.ctx, collections.Join(opStr, "unbond-peer"))
	require.Equal(t, types.BridgeStatus_BRIDGE_STATUS_UNBONDING, bridge.Status)

	// Double-unbond fails
	_, err = ms.UnbondBridge(f.ctx, &types.MsgUnbondBridge{
		Operator: opStr, PeerId: "unbond-peer",
	})
	require.Error(t, err)
}
