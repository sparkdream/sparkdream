package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// The peer lifecycle is deliberately asymmetric: starting a trust
// relationship takes a committee vote, stopping one takes a single
// signature. These tests pin that asymmetry. See
// docs/x-federation-peer-authorization-plan.md.
//
// The fixture's default commons stub authorizes everyone, so every test
// here replaces it with one that tells the three real principals apart.
// Without that replacement none of these assertions would prove anything.

// authzFixture returns a fixture whose commons stub models the real
// wiring: the committee policy address and the council policy address are
// governance actors, an individual committee member is not -- but the
// member still satisfies the looser IsCouncilAuthorized, exactly as they
// would on chain.
func authzFixture(t *testing.T) (f *fixture, committeePolicy, councilPolicy, individualMember string) {
	t.Helper()
	f = initFixture(t)
	committeePolicy = testAddr(t, f, "ops-committee-policy")
	councilPolicy = testAddr(t, f, "commons-council-policy")
	individualMember = testAddr(t, f, "ops-committee-member")

	policies := map[string]bool{committeePolicy: true, councilPolicy: true}
	f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, _, _ string) bool {
		return policies[addr]
	}
	f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, addr, _, _ string) bool {
		return policies[addr] || addr == individualMember
	}
	return f, committeePolicy, councilPolicy, individualMember
}

// pendingPeer registers a peer and leaves it PENDING -- registerTestPeer
// activates as part of the normal lifecycle, which is what these tests
// are trying to exercise by hand.
func pendingPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	registerTestPeer(t, f, ms, peerID)
	peer, err := f.keeper.Peers.Get(f.ctx, peerID)
	require.NoError(t, err)
	peer.Status = types.PeerStatus_PEER_STATUS_PENDING
	require.NoError(t, f.keeper.Peers.Set(f.ctx, peerID, peer))
}

func TestResumePeerRequiresCommitteePolicy(t *testing.T) {
	// The rejected case is the whole point of the hardening: an
	// individual Operations Committee member can no longer activate a
	// peer on their own signature.
	t.Run("individual committee member is rejected", func(t *testing.T) {
		f, _, _, member := authzFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		pendingPeer(t, f, ms, "phoenix.example")

		_, err := ms.ResumePeer(f.ctx, &types.MsgResumePeer{
			Authority: member, PeerId: "phoenix.example",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "individual committee member is not enough")

		peer, err := f.keeper.Peers.Get(f.ctx, "phoenix.example")
		require.NoError(t, err)
		require.Equal(t, types.PeerStatus_PEER_STATUS_PENDING, peer.Status,
			"a rejected activation must leave the peer inert")
	})

	accepted := []struct {
		name string
		addr func(f *fixture, committee, council string) string
	}{
		{"committee policy", func(_ *fixture, committee, _ string) string { return committee }},
		{"council policy", func(_ *fixture, _, council string) string { return council }},
		{"gov authority", func(f *fixture, _, _ string) string { return f.authority }},
	}
	for _, tc := range accepted {
		t.Run(tc.name+" is accepted", func(t *testing.T) {
			f, committee, council, _ := authzFixture(t)
			ms := keeper.NewMsgServerImpl(f.keeper)
			pendingPeer(t, f, ms, "aurora.example")

			_, err := ms.ResumePeer(f.ctx, &types.MsgResumePeer{
				Authority: tc.addr(f, committee, council), PeerId: "aurora.example",
			})
			require.NoError(t, err)

			peer, err := f.keeper.Peers.Get(f.ctx, "aurora.example")
			require.NoError(t, err)
			require.Equal(t, types.PeerStatus_PEER_STATUS_ACTIVE, peer.Status)
		})
	}
}

// TestSuspendPeerStillAcceptsIndividualMember pins the emergency brake.
// The address asserted here is the same one TestResumePeerRequiresCommitteePolicy
// rejects, so a later uniformity pass cannot tighten SuspendPeer without
// failing this test.
func TestSuspendPeerStillAcceptsIndividualMember(t *testing.T) {
	f, _, _, member := authzFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "zenith.example")

	_, err := ms.SuspendPeer(f.ctx, &types.MsgSuspendPeer{
		Authority: member, PeerId: "zenith.example", Reason: "suspected spam",
	})
	require.NoError(t, err, "a single committee member must be able to pull the emergency brake")

	peer, err := f.keeper.Peers.Get(f.ctx, "zenith.example")
	require.NoError(t, err)
	require.Equal(t, types.PeerStatus_PEER_STATUS_SUSPENDED, peer.Status)
}

// TestRegisterBridgeRequiresActivePeer closes the second activation path:
// a bridge operator used to flip their own PENDING peer to ACTIVE simply
// by binding to it, which made the lower-trust integration the easier one
// to activate.
func TestRegisterBridgeRequiresActivePeer(t *testing.T) {
	f, committee, _, _ := authzFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	pendingPeer(t, f, ms, "vega.example")
	opStr := testAddr(t, f, "bridge-operator")

	bind := &types.MsgRegisterBridge{
		Operator: opStr, PeerId: "vega.example",
		Protocol: "activitypub", Endpoint: "https://bridge.vega.example",
	}

	_, err := ms.RegisterBridge(f.ctx, bind)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrPeerNotActive)
	require.Contains(t, err.Error(), "must be activated (MsgResumePeer)")

	peer, err := f.keeper.Peers.Get(f.ctx, "vega.example")
	require.NoError(t, err)
	require.Equal(t, types.PeerStatus_PEER_STATUS_PENDING, peer.Status,
		"a rejected binding must not activate the peer")

	// Once governance activates it, the same binding is accepted.
	_, err = ms.ResumePeer(f.ctx, &types.MsgResumePeer{
		Authority: committee, PeerId: "vega.example",
	})
	require.NoError(t, err)

	_, err = ms.RegisterBridge(f.ctx, bind)
	require.NoError(t, err)
}
