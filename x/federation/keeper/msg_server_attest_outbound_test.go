package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestAttestOutbound(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "attest-peer")
	opStr := registerTestBridge(t, f, ms, "attest-peer", "attest-op")

	_, err := ms.AttestOutbound(f.ctx, &types.MsgAttestOutbound{
		Operator: opStr, PeerId: "attest-peer",
		ContentType: "blog_post", LocalContentId: "local-42",
	})
	require.NoError(t, err)
}

func TestAttestOutboundWrongContentType(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "attest-peer2")
	opStr := registerTestBridge(t, f, ms, "attest-peer2", "attest-op2")

	// collection not in outbound types
	_, err := ms.AttestOutbound(f.ctx, &types.MsgAttestOutbound{
		Operator: opStr, PeerId: "attest-peer2",
		ContentType: "collection", LocalContentId: "local-99",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in outbound")
}
