package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestLinkIdentity(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "link-peer")

	peer, _ := f.keeper.Peers.Get(f.ctx, "link-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "link-peer", peer))

	userStr := testAddr(t, f, "link-user")

	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: userStr, PeerId: "link-peer", RemoteIdentity: "@alice@mastodon.example",
	})
	require.NoError(t, err)

	// Verify link stored
	link, err := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(userStr, "link-peer"))
	require.NoError(t, err)
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED, link.Status)
	// FEDERATION-S2-1: a non-empty challenge must be persisted so the
	// confirmation packet can be echo-checked when it arrives.
	require.NotEmpty(t, link.Challenge, "expected stored challenge for echo-check")

	// FED-28: an unverified claim is indexed as a claim only; it does not
	// occupy the verified reverse index.
	has, err := f.keeper.IdentityClaimsByRemote.Has(f.ctx, collections.Join3("link-peer", "@alice@mastodon.example", userStr))
	require.NoError(t, err)
	require.True(t, has)
	has, _ = f.keeper.IdentityLinksByRemote.Has(f.ctx, collections.Join("link-peer", "@alice@mastodon.example"))
	require.False(t, has)

	// Count incremented
	count, _ := f.keeper.IdentityLinkCount.Get(f.ctx, userStr)
	require.Equal(t, uint32(1), count)
}

func TestLinkIdentityDuplicate(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "dup-link-peer")
	peer, _ := f.keeper.Peers.Get(f.ctx, "dup-link-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "dup-link-peer", peer))

	userStr := testAddr(t, f, "dup-user")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: userStr, PeerId: "dup-link-peer", RemoteIdentity: "@bob@example.com",
	})
	require.NoError(t, err)

	_, err = ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: userStr, PeerId: "dup-link-peer", RemoteIdentity: "@bob2@example.com",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

// FED-28: an unverified claim never reserves a remote identity, so a
// squatter cannot lock the real owner out. Several unverified claims may
// coexist; once one verifies, the identity is taken and nobody else can
// claim or verify it.
func TestLinkIdentityRemoteClaimed(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	setupIBCPeerForLink(t, f, ms, "claimed-peer", "channel-5")

	squatter := testAddr(t, f, "claim-squatter")
	owner := testAddr(t, f, "claim-owner")
	late := testAddr(t, f, "claim-late")

	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: squatter, PeerId: "claimed-peer", RemoteIdentity: "sprkdrm1shared",
	})
	require.NoError(t, err)
	// The real owner can still claim while the squatter's claim is unverified.
	_, err = ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: owner, PeerId: "claimed-peer", RemoteIdentity: "sprkdrm1shared",
	})
	require.NoError(t, err)

	// The remote chain confirms the owner's challenge.
	ownerLink, err := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(owner, "claimed-peer"))
	require.NoError(t, err)
	require.NoError(t, f.keeper.OnRecvIdentityConfirmPacket(f.ctx, "federation", "channel-5", &types.IdentityVerificationConfirmPacket{
		ClaimedAddress: "sprkdrm1shared", ClaimantAddress: owner, Challenge: ownerLink.Challenge, Confirmed: true,
	}))
	resolved, err := f.keeper.IdentityLinksByRemote.Get(f.ctx, collections.Join("claimed-peer", "sprkdrm1shared"))
	require.NoError(t, err)
	require.Equal(t, owner, resolved)

	// Now the identity is taken: no new claim, and the squatter's pending
	// claim cannot verify on top of it.
	_, err = ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{
		Creator: late, PeerId: "claimed-peer", RemoteIdentity: "sprkdrm1shared",
	})
	require.ErrorIs(t, err, types.ErrRemoteIdentityAlreadyClaimed)
	squatLink, err := f.keeper.IdentityLinks.Get(f.ctx, collections.Join(squatter, "claimed-peer"))
	require.NoError(t, err)
	err = f.keeper.OnRecvIdentityConfirmPacket(f.ctx, "federation", "channel-5", &types.IdentityVerificationConfirmPacket{
		ClaimedAddress: "sprkdrm1shared", ClaimantAddress: squatter, Challenge: squatLink.Challenge, Confirmed: true,
	})
	require.ErrorIs(t, err, types.ErrRemoteIdentityAlreadyClaimed)

	// The squatter unlinking leaves the owner's verified entry alone.
	_, err = ms.UnlinkIdentity(f.ctx, &types.MsgUnlinkIdentity{Creator: squatter, PeerId: "claimed-peer"})
	require.NoError(t, err)
	resolved, err = f.keeper.IdentityLinksByRemote.Get(f.ctx, collections.Join("claimed-peer", "sprkdrm1shared"))
	require.NoError(t, err)
	require.Equal(t, owner, resolved)
}

// FED-30: a Spark Dream peer's challenge that cannot be sent fails the tx
// (no link is left without a retry path); a bridge peer sends nothing.
func TestLinkIdentitySendFailure(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	setupIBCPeerForLink(t, f, ms, "send-peer", "channel-6")
	f.keeper.SetSendPacketOverrideForTest(func(context.Context, string, []byte) (uint64, error) {
		return 0, types.ErrIBCNotAvailable
	})
	user := testAddr(t, f, "send-user")
	_, err := ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{Creator: user, PeerId: "send-peer", RemoteIdentity: "sprkdrm1x"})
	require.ErrorIs(t, err, types.ErrIBCNotAvailable)

	// A bridge peer never sends, so a failing sender does not matter.
	registerTestPeer(t, f, ms, "bridge-link-peer")
	peer, _ := f.keeper.Peers.Get(f.ctx, "bridge-link-peer")
	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "bridge-link-peer", peer))
	_, err = ms.LinkIdentity(f.ctx, &types.MsgLinkIdentity{Creator: user, PeerId: "bridge-link-peer", RemoteIdentity: "@x@bridge.example"})
	require.NoError(t, err)
}
