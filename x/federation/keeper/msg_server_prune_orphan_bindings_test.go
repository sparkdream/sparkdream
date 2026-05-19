package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	servicetypes "sparkdream/x/service/types"
)

func TestMsgPruneOrphanBindings_RemovesOrphansLeavesLiveAlone(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sk := wireServiceKeeper(t, f)

	registerTestPeer(t, f, ms, "mastodon.social")
	const peer = "mastodon.social"
	st := types.ServiceTypeFederationBridgeActivityPub
	opAlive := testAddr(t, f, "op-alive")
	opOrphan := testAddr(t, f, "op-orphan")
	seedBinding(t, f, opAlive, peer, st)
	seedBinding(t, f, opOrphan, peer, st)

	// Only the alive operator has a service.Operator.
	sk.Operators[sk.opKey(opAlive, st)] = servicetypes.Operator{
		Address:     opAlive,
		ServiceType: st,
		Status:      servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE,
	}
	sk.GetOperatorFn = func(_ context.Context, addrBytes []byte, serviceType string) (servicetypes.Operator, bool) {
		s, _ := f.addressCodec.BytesToString(addrBytes)
		o, ok := sk.Operators[s+"/"+serviceType]
		return o, ok
	}

	resp, err := ms.PruneOrphanBindings(f.ctx, &types.MsgPruneOrphanBindings{
		Authority: f.authority,
		PeerId:    peer,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, resp.Pruned)

	// Orphan binding should be gone, alive binding should remain.
	_, err = f.keeper.BridgeBindings.Get(f.ctx, collections.Join(opOrphan, peer))
	require.Error(t, err, "orphan binding should be pruned")
	_, err = f.keeper.BridgeBindings.Get(f.ctx, collections.Join(opAlive, peer))
	require.NoError(t, err, "alive binding should remain")
}
