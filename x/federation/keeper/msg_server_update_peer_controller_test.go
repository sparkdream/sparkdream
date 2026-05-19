package keeper_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestMsgUpdatePeerController_GovAuthorityOnly(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "mastodon.social")

	newCtl := testAddr(t, f, "new-controller")

	// Non-gov caller is rejected.
	_, err := ms.UpdatePeerController(f.ctx, &types.MsgUpdatePeerController{
		Authority:       testAddr(t, f, "random"),
		PeerId:          "mastodon.social",
		ControllerGroup: newCtl,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, types.ErrNotAuthorized) || errorContainsAny(err, "x/gov authority", "must be"))
}

func TestMsgUpdatePeerController_PeerMustExist(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	newCtl := testAddr(t, f, "new-controller")
	_, err := ms.UpdatePeerController(f.ctx, &types.MsgUpdatePeerController{
		Authority:       f.authority,
		PeerId:          "nonexistent",
		ControllerGroup: newCtl,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, types.ErrPeerNotFound) || errorContainsAny(err, "not found"))
}

func TestMsgUpdatePeerController_ValidatesGroupPolicyAddress(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "mastodon.social")

	// Override the commons mock to reject the proposed controller.
	mockCommons := &mockCommonsKeeper{
		IsGroupPolicyAddressFn: func(_ context.Context, _ string) bool { return false },
	}
	f.keeper.SetCommonsKeeper(mockCommons)

	_, err := ms.UpdatePeerController(f.ctx, &types.MsgUpdatePeerController{
		Authority:       f.authority,
		PeerId:          "mastodon.social",
		ControllerGroup: testAddr(t, f, "not-a-group"),
	})
	require.Error(t, err)
	require.True(t, errorContainsAny(err, "not a registered group policy address"))
}

func TestMsgUpdatePeerController_SetsField(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "mastodon.social")

	newCtl := testAddr(t, f, "new-controller")
	_, err := ms.UpdatePeerController(f.ctx, &types.MsgUpdatePeerController{
		Authority:       f.authority,
		PeerId:          "mastodon.social",
		ControllerGroup: newCtl,
	})
	require.NoError(t, err)

	peer, err := f.keeper.Peers.Get(f.ctx, "mastodon.social")
	require.NoError(t, err)
	require.Equal(t, newCtl, peer.ControllerGroup)
}
