package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// FED-09: a confirmation whose packet cannot be sent fails the tx (it used
// to be dropped silently while the challenge was deleted), and the pending
// challenge is kept so the user can re-confirm. The unit fixture has no
// open IBC channel, so SendPacket fails here by construction.
func TestConfirmIdentityLink(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	setupIBCPeerForLink(t, f, ms, "remote-chain", "channel-4")
	f.keeper.SetSendPacketOverrideForTest(nil) // the real, failing send

	claimedAddr := testAddr(t, f, "claimed-addr")
	claimantPeer := "remote-chain"

	// Pre-populate a pending challenge
	challenge := types.PendingIdentityChallenge{
		ClaimedAddress:      claimedAddr,
		ClaimantChainPeerId: claimantPeer,
		ClaimantAddress:     "remote-user",
		Challenge:           []byte("random-challenge-bytes"),
		ReceivedAt:          100,
		ExpiresAt:           9999999999, // far future (year 2286)
	}
	require.NoError(t, f.keeper.PendingIdChallenges.Set(f.ctx, collections.Join(claimedAddr, claimantPeer), challenge))

	_, err := ms.ConfirmIdentityLink(f.ctx, &types.MsgConfirmIdentityLink{
		Creator: claimedAddr, ClaimantChainPeerId: claimantPeer,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "send identity confirmation")

	// The challenge survives, so a later re-confirm can still go out.
	_, err = f.keeper.PendingIdChallenges.Get(f.ctx, collections.Join(claimedAddr, claimantPeer))
	require.NoError(t, err)
}

// FED-09: no confirmations to a peer that is not ACTIVE.
func TestConfirmIdentityLinkPeerNotActive(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	setupIBCPeerForLink(t, f, ms, "remote-chain", "channel-4")
	peer, _ := f.keeper.Peers.Get(f.ctx, "remote-chain")
	peer.Status = types.PeerStatus_PEER_STATUS_SUSPENDED
	require.NoError(t, f.keeper.Peers.Set(f.ctx, "remote-chain", peer))

	claimedAddr := testAddr(t, f, "claimed-addr")
	require.NoError(t, f.keeper.PendingIdChallenges.Set(f.ctx, collections.Join(claimedAddr, "remote-chain"), types.PendingIdentityChallenge{
		ClaimedAddress: claimedAddr, ClaimantChainPeerId: "remote-chain", ClaimantAddress: "remote-user",
		Challenge: []byte("c"), ExpiresAt: 9999999999,
	}))
	_, err := ms.ConfirmIdentityLink(f.ctx, &types.MsgConfirmIdentityLink{Creator: claimedAddr, ClaimantChainPeerId: "remote-chain"})
	require.ErrorIs(t, err, types.ErrPeerNotActive)
}

func TestConfirmIdentityLinkNoPending(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	addr := testAddr(t, f, "no-pending")
	_, err := ms.ConfirmIdentityLink(f.ctx, &types.MsgConfirmIdentityLink{
		Creator: addr, ClaimantChainPeerId: "nonexistent",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending")
}
