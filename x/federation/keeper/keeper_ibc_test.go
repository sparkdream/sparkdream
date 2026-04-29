package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// FEDERATION-S2-1: confirmation packets are now bound to the link via two
// echo-checks (claimed_address, challenge). These tests cover the new branches
// added to OnRecvIdentityConfirmPacket.

func setupIBCPeerForLink(t *testing.T, f *fixture, ms types.MsgServer, peerID, channelID string) {
	t.Helper()
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority:    f.authority,
		PeerId:       peerID,
		DisplayName:  "IBC Peer " + peerID,
		Type:         types.PeerType_PEER_TYPE_SPARK_DREAM,
		IbcChannelId: channelID,
	})
	require.NoError(t, err)
	peer, _ := f.keeper.Peers.Get(f.ctx, peerID)
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, peerID, peer))
}

func TestOnRecvIdentityConfirmPacket_HappyPath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	setupIBCPeerForLink(t, f, ms, "ibc-peer", "channel-1")

	user := testAddr(t, f, "confirm-happy")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: user, PeerId: "ibc-peer", RemoteIdentity: "sprkdrm1remote",
	})
	require.NoError(t, err)

	// Recover the issued challenge.
	link, err := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(user, "ibc-peer"))
	require.NoError(t, err)
	require.NotEmpty(t, link.Challenge)

	// Honest confirmation packet from the remote chain echoes the challenge
	// and the claimed address.
	err = f.keeper.OnRecvIdentityConfirmPacket(f.ctx, "channel-1", &types.IdentityVerificationConfirmPacket{
		ClaimedAddress:  "sprkdrm1remote",
		ClaimantAddress: user,
		Challenge:       link.Challenge,
		Confirmed:       true,
	})
	require.NoError(t, err)

	verified, err := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(user, "ibc-peer"))
	require.NoError(t, err)
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED, verified.Status)
}

func TestOnRecvIdentityConfirmPacket_RejectsClaimedAddressMismatch(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	setupIBCPeerForLink(t, f, ms, "ibc-peer", "channel-2")

	user := testAddr(t, f, "confirm-claim")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: user, PeerId: "ibc-peer", RemoteIdentity: "sprkdrm1real",
	})
	require.NoError(t, err)
	link, _ := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(user, "ibc-peer"))

	// Confirmation packet swaps in a different remote identity.
	err = f.keeper.OnRecvIdentityConfirmPacket(f.ctx, "channel-2", &types.IdentityVerificationConfirmPacket{
		ClaimedAddress:  "sprkdrm1evil",
		ClaimantAddress: user,
		Challenge:       link.Challenge,
		Confirmed:       true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "claimed_address")

	stillUnverified, _ := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(user, "ibc-peer"))
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED, stillUnverified.Status)
}

func TestOnRecvIdentityConfirmPacket_RejectsChallengeMismatch(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	setupIBCPeerForLink(t, f, ms, "ibc-peer", "channel-3")

	user := testAddr(t, f, "confirm-chal")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: user, PeerId: "ibc-peer", RemoteIdentity: "sprkdrm1remote",
	})
	require.NoError(t, err)

	// Forged challenge bytes.
	err = f.keeper.OnRecvIdentityConfirmPacket(f.ctx, "channel-3", &types.IdentityVerificationConfirmPacket{
		ClaimedAddress:  "sprkdrm1remote",
		ClaimantAddress: user,
		Challenge:       []byte("not-the-real-challenge"),
		Confirmed:       true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "challenge")

	stillUnverified, _ := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(user, "ibc-peer"))
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED, stillUnverified.Status)
}
