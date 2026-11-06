package keeper_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	servicetypes "sparkdream/x/service/types"
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

// FED-107: AttestOutbound applies the operator gate of every other bridge
// message, bounds local_content_id and is rate limited per peer.
func TestAttestOutboundGates(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "attest-peer3")
	opStr := registerTestBridge(t, f, ms, "attest-peer3", "attest-op3")
	attest := func(id string) error {
		_, err := ms.AttestOutbound(f.ctx, &types.MsgAttestOutbound{
			Operator: opStr, PeerId: "attest-peer3", ContentType: "blog_post", LocalContentId: id,
		})
		return err
	}

	require.ErrorIs(t, attest(strings.Repeat("x", types.MaxInboundIdentifierSize+1)), types.ErrInvalidRequest)
	require.ErrorIs(t, attest(""), types.ErrInvalidRequest)

	policy, err := f.keeper.PeerPolicies.Get(f.ctx, "attest-peer3")
	require.NoError(t, err)
	policy.OutboundRateLimitPerEpoch = 1
	require.NoError(t, f.keeper.PeerPolicies.Set(f.ctx, "attest-peer3", policy))
	require.NoError(t, attest("local-1"))
	require.Error(t, attest("local-2"), "second attestation in the window is rate limited")

	sk := wireServiceKeeper(t, f)
	sk.GetOperatorFn = func(context.Context, []byte, string) (servicetypes.Operator, bool) {
		return servicetypes.Operator{Status: servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING}, true
	}
	require.ErrorIs(t, attest("local-3"), types.ErrBindingNotActive)
}
