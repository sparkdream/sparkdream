package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func TestConfirmIdentityLink(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	claimedAddr := testAddr(t, f, "claimed-addr")
	claimantPeer := "remote-chain"

	// Pre-populate a pending challenge
	challenge := types.PendingIdentityChallenge{
		ClaimedAddress:     claimedAddr,
		ClaimantChainPeerId: claimantPeer,
		ClaimantAddress:    "remote-user",
		Challenge:          []byte("random-challenge-bytes"),
		ReceivedAt:         100,
		ExpiresAt:          9999999999, // far future (year 2286)
	}
	require.NoError(t, f.keeper.PendingIdChallenges.Set(f.ctx, collections.Join(claimedAddr, claimantPeer), challenge))

	_, err := ms.ConfirmIdentityLink(f.ctx, &types.MsgConfirmIdentityLink{
		Creator: claimedAddr, ClaimantChainPeerId: claimantPeer,
	})
	require.NoError(t, err)

	// Challenge should be deleted
	_, err = f.keeper.PendingIdChallenges.Get(f.ctx, collections.Join(claimedAddr, claimantPeer))
	require.Error(t, err)
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
