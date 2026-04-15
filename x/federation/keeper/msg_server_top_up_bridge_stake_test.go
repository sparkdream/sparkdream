package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestTopUpBridgeStake(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "topup-peer")
	opStr := registerTestBridge(t, f, ms, "topup-peer", "topup-op")

	original, _ := f.keeper.BridgeOperators.Get(f.ctx, collections.Join(opStr, "topup-peer"))
	originalAmount := original.Stake.Amount

	_, err := ms.TopUpBridgeStake(f.ctx, &types.MsgTopUpBridgeStake{
		Operator: opStr, PeerId: "topup-peer",
		Amount: sdk.NewCoin("uspark", math.NewInt(500_000_000)),
	})
	require.NoError(t, err)

	bridge, _ := f.keeper.BridgeOperators.Get(f.ctx, collections.Join(opStr, "topup-peer"))
	require.True(t, bridge.Stake.Amount.GT(originalAmount))
}

func TestTopUpBridgeStakeWrongDenom(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "topup-peer2")
	opStr := registerTestBridge(t, f, ms, "topup-peer2", "topup-op2")

	_, err := ms.TopUpBridgeStake(f.ctx, &types.MsgTopUpBridgeStake{
		Operator: opStr, PeerId: "topup-peer2",
		Amount: sdk.NewCoin("udream", math.NewInt(100)),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "denomination")
}
