package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestRevokeBridge(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "revoke-peer")
	opStr := registerTestBridge(t, f, ms, "revoke-peer", "revoke-op")

	_, err := ms.RevokeBridge(f.ctx, &types.MsgRevokeBridge{
		Authority: f.authority, Operator: opStr, PeerId: "revoke-peer", Reason: "misbehavior",
	})
	require.NoError(t, err)

	bridge, _ := f.keeper.BridgeOperators.Get(f.ctx, collections.Join(opStr, "revoke-peer"))
	require.Equal(t, types.BridgeStatus_BRIDGE_STATUS_UNBONDING, bridge.Status)
	require.NotZero(t, bridge.UnbondingEndTime)
}
