package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestRegisterBridge(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "mastodon.social")

	opStr := testAddr(t, f, "operator1")

	_, err := ms.RegisterBridge(f.ctx, &types.MsgRegisterBridge{
		Authority: f.authority, Operator: opStr, PeerId: "mastodon.social",
		Protocol: "activitypub", Endpoint: "https://bridge.example.com",
	})
	require.NoError(t, err)

	bridge, err := f.keeper.BridgeOperators.Get(f.ctx, collections.Join(opStr, "mastodon.social"))
	require.NoError(t, err)
	require.Equal(t, types.BridgeStatus_BRIDGE_STATUS_ACTIVE, bridge.Status)

	// Peer should be ACTIVE now
	peer, _ := f.keeper.Peers.Get(f.ctx, "mastodon.social")
	require.Equal(t, types.PeerStatus_PEER_STATUS_ACTIVE, peer.Status)

	// Duplicate fails
	_, err = ms.RegisterBridge(f.ctx, &types.MsgRegisterBridge{
		Authority: f.authority, Operator: opStr, PeerId: "mastodon.social",
		Protocol: "activitypub",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already registered")
}

func TestRegisterBridgeWrongPeerType(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestIBCPeer(t, f, ms, "sparkdream-2")

	opStr := testAddr(t, f, "operator2")
	_, err := ms.RegisterBridge(f.ctx, &types.MsgRegisterBridge{
		Authority: f.authority, Operator: opStr, PeerId: "sparkdream-2", Protocol: "activitypub",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ActivityPub/AT Protocol")
}
