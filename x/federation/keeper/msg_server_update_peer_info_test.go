package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestUpdatePeerInfo(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "info-peer")
	before, err := f.keeper.Peers.Get(f.ctx, "info-peer")
	require.NoError(t, err)

	_, err = ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{
		Authority: f.authority, PeerId: "info-peer",
		DisplayName: "Spark Dream Mastodon", Metadata: `{"software":"mastodon"}`,
	})
	require.NoError(t, err)

	after, err := f.keeper.Peers.Get(f.ctx, "info-peer")
	require.NoError(t, err)
	require.Equal(t, "Spark Dream Mastodon", after.DisplayName)
	require.Equal(t, `{"software":"mastodon"}`, after.Metadata)
	// Nothing else moves.
	after.DisplayName, after.Metadata = before.DisplayName, before.Metadata
	require.Equal(t, before, after)
}

func TestUpdatePeerInfoRejectsUnauthorized(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "info-peer")
	f.keeper.SetCommonsKeeper(&mockCommonsKeeper{
		IsCouncilAuthorizedFn: func(context.Context, string, string, string) bool { return false },
	})

	_, err := ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{
		Authority: testAddr(t, f, "random"), PeerId: "info-peer", DisplayName: "x",
	})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
}

func TestUpdatePeerInfoRefusesMissingAndRemovedPeers(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{
		Authority: f.authority, PeerId: "nonexistent", DisplayName: "x",
	})
	require.ErrorIs(t, err, types.ErrPeerNotFound)

	registerTestPeer(t, f, ms, "removed-peer")
	peer, err := f.keeper.Peers.Get(f.ctx, "removed-peer")
	require.NoError(t, err)
	peer.Status = types.PeerStatus_PEER_STATUS_REMOVED
	require.NoError(t, f.keeper.SetPeer(f.ctx, peer))

	_, err = ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{
		Authority: f.authority, PeerId: "removed-peer", DisplayName: "x",
	})
	require.ErrorIs(t, err, types.ErrPeerNotActive)
}

// FED-106: expected_chain_id may be corrected only on a Spark Dream peer
// that is not ACTIVE; an empty value keeps the current pin.
func TestUpdatePeerInfoExpectedChainID(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	// PENDING: correctable.
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "phoenix", DisplayName: "Phoenix",
		Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-4", ExpectedChainId: "phoenix-0",
	})
	require.NoError(t, err)
	upd := &types.MsgUpdatePeerInfo{Authority: f.authority, PeerId: "phoenix", DisplayName: "Phoenix", ExpectedChainId: "phoenix-1"}
	require.NoError(t, upd.ValidateBasic())
	_, err = ms.UpdatePeerInfo(f.ctx, upd)
	require.NoError(t, err)
	peer, err := f.keeper.Peers.Get(f.ctx, "phoenix")
	require.NoError(t, err)
	require.Equal(t, "phoenix-1", peer.ExpectedChainId)

	// Empty keeps the pin.
	_, err = ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{Authority: f.authority, PeerId: "phoenix", DisplayName: "Phoenix Chain"})
	require.NoError(t, err)
	peer, err = f.keeper.Peers.Get(f.ctx, "phoenix")
	require.NoError(t, err)
	require.Equal(t, "phoenix-1", peer.ExpectedChainId)
	require.Equal(t, "Phoenix Chain", peer.DisplayName)

	// Malformed is refused in ValidateBasic and the handler.
	bad := &types.MsgUpdatePeerInfo{Authority: f.authority, PeerId: "phoenix", ExpectedChainId: "phoenix 2"}
	require.ErrorContains(t, bad.ValidateBasic(), "whitespace")
	_, err = ms.UpdatePeerInfo(f.ctx, bad)
	require.ErrorContains(t, err, "whitespace")

	// ACTIVE: refused, the pin is what activation verified.
	activateTestPeer(t, f, ms, "phoenix")
	_, err = ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{Authority: f.authority, PeerId: "phoenix", ExpectedChainId: "phoenix-2"})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	require.ErrorContains(t, err, "ACTIVE")
	peer, err = f.keeper.Peers.Get(f.ctx, "phoenix")
	require.NoError(t, err)
	require.Equal(t, "phoenix-1", peer.ExpectedChainId)

	// SUSPENDED: correctable again; the next activation re-verifies it.
	_, err = ms.SuspendPeer(f.ctx, &types.MsgSuspendPeer{Authority: f.authority, PeerId: "phoenix", Reason: "rotate"})
	require.NoError(t, err)
	_, err = ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{Authority: f.authority, PeerId: "phoenix", ExpectedChainId: "phoenix-2"})
	require.NoError(t, err)
	_, err = ms.ResumePeer(f.ctx, &types.MsgResumePeer{Authority: f.authority, PeerId: "phoenix"})
	require.ErrorIs(t, err, types.ErrPeerChainIdMismatch, "channel still leads to phoenix-1")

	// Not a Spark Dream peer: refused.
	registerTestPeer(t, f, ms, "pleroma.example")
	_, err = ms.UpdatePeerInfo(f.ctx, &types.MsgUpdatePeerInfo{Authority: f.authority, PeerId: "pleroma.example", ExpectedChainId: "pleroma-1"})
	require.ErrorContains(t, err, "only for Spark Dream peers")
}
