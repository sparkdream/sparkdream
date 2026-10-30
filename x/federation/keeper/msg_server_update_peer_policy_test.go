package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestUpdatePeerPolicy(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "policy-peer")

	// Valid update
	_, err := ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority, PeerId: "policy-peer",
		Policy: types.PeerPolicy{InboundContentTypes: []string{"blog_post"}, OutboundContentTypes: []string{"forum_thread"}},
	})
	require.NoError(t, err)
	policy, _ := f.keeper.PeerPolicies.Get(f.ctx, "policy-peer")
	require.Equal(t, []string{"blog_post"}, policy.InboundContentTypes)

	// Reveal content types rejected
	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority, PeerId: "policy-peer",
		Policy: types.PeerPolicy{InboundContentTypes: []string{"reveal_proposal"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reveal")

	// Reputation on non-Spark Dream rejected
	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority, PeerId: "policy-peer",
		Policy: types.PeerPolicy{AllowReputationQueries: true},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reputation")

	// Unknown content type rejected
	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority, PeerId: "policy-peer",
		Policy: types.PeerPolicy{InboundContentTypes: []string{"unknown_type"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

// A REMOVED peer's policy cannot be rewritten: the removal cleanup deletes
// the policy once, so a later write would outlive the peer.
func TestUpdatePeerPolicyRefusesRemovedPeer(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "removed-peer")
	peer, err := f.keeper.Peers.Get(f.ctx, "removed-peer")
	require.NoError(t, err)
	peer.Status = types.PeerStatus_PEER_STATUS_REMOVED
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "removed-peer", peer))

	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority, PeerId: "removed-peer",
		Policy: types.PeerPolicy{InboundContentTypes: []string{"blog_post"}},
	})
	require.ErrorIs(t, err, types.ErrPeerNotActive)
}
