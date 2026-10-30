package keeper_test

import (
	"crypto/sha256"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// FED-13: bridge operators must be active members, so the alias-aware
// self-dealing checks see the person behind a bridge. A non-member cannot
// register one, and an operator whose membership lapses (zeroed) stops
// submitting, attesting, arbitrating and earning.

func TestRegisterBridgeRequiresMember(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.social")
	outsider := testAddr(t, f, "outsider-op")
	f.repKeeper.inactive = map[string]bool{outsider: true}

	_, err := ms.RegisterBridge(f.ctx, &types.MsgRegisterBridge{
		Operator: outsider, PeerId: "phoenix.social", Protocol: "activitypub",
		Endpoint: "https://outsider.example.com", StakeAmount: math.ZeroInt(),
	})
	require.ErrorIs(t, err, types.ErrOperatorNotMember)
}

func TestZeroedOperatorIsSuspended(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "phoenix.social")
	op := registerTestBridge(t, f, ms, "phoenix.social", "zeroed-op")
	h := sha256.Sum256([]byte("before zeroing"))
	submitTestContent(t, f, ms, op, "phoenix.social", h[:])

	f.repKeeper.inactive = map[string]bool{op: true}

	h2 := sha256.Sum256([]byte("after zeroing"))
	_, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		License: "CC0-1.0", Operator: op, PeerId: "phoenix.social", ContentType: "blog_post",
		CreatorIdentity: "@a@phoenix.social", ContentHash: h2[:],
	})
	require.ErrorIs(t, err, types.ErrOperatorNotMember)

	_, err = ms.AttestOutbound(f.ctx, &types.MsgAttestOutbound{
		Operator: op, PeerId: "phoenix.social", ContentType: "blog_post", LocalContentId: "local-1",
	})
	require.ErrorIs(t, err, types.ErrOperatorNotMember)
}

func TestZeroedOperatorEarnsNothing(t *testing.T) {
	f := initFixture(t)
	member := testAddr(t, f, "op-member")
	zeroed := testAddr(t, f, "op-zeroed")
	seedRewardBinding(t, f, member, "peer-a", 10, 10, 0, 0)
	seedRewardBinding(t, f, zeroed, "peer-b", 10, 10, 0, 0)
	f.repKeeper.inactive = map[string]bool{zeroed: true}
	fundOperatorPool(f, 1_000_000)

	ctx := operatorEpochCtx(t, f)
	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))
	require.Equal(t, "1000000", operatorBalance(f, member).String(), "the member operator takes the whole pool")
	require.True(t, operatorBalance(f, zeroed).IsZero())
}
