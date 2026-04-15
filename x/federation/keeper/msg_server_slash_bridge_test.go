package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestSlashBridge(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "slash-peer")
	opStr := registerTestBridge(t, f, ms, "slash-peer", "slash-op")

	halfStake := types.DefaultParams().MinBridgeStake.Amount.Quo(math.NewInt(2))
	_, err := ms.SlashBridge(f.ctx, &types.MsgSlashBridge{
		Authority: f.authority, Operator: opStr, PeerId: "slash-peer",
		Amount: halfStake, Reason: "spam",
	})
	require.NoError(t, err)

	bridge, _ := f.keeper.BridgeOperators.Get(f.ctx, collections.Join(opStr, "slash-peer"))
	require.Equal(t, uint64(1), bridge.SlashCount)
	// Auto-revoked since remaining < min
	require.Equal(t, types.BridgeStatus_BRIDGE_STATUS_UNBONDING, bridge.Status)
}

func TestSlashBridgeExceedsStake(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "slash-peer2")
	opStr := registerTestBridge(t, f, ms, "slash-peer2", "slash-op2")

	_, err := ms.SlashBridge(f.ctx, &types.MsgSlashBridge{
		Authority: f.authority, Operator: opStr, PeerId: "slash-peer2",
		Amount: types.DefaultParams().MinBridgeStake.Amount.MulRaw(2), Reason: "too much",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds stake")
}
