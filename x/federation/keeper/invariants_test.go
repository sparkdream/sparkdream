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

// Tests for x/federation/keeper/invariants.go — the orphan-binding and
// bindings-by-operator-index invariants registered with x/crisis.

func TestOrphanBindingsInvariant_FlagsBindingWithMissingOperator(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sk := wireServiceKeeper(t, f)

	// Set up: peer + binding, but NO service.Operator (orphan).
	registerTestPeer(t, f, ms, "mastodon.social")
	op := testAddr(t, f, "op-orphan")
	seedBinding(t, f, op, "mastodon.social", types.ServiceTypeFederationBridgeActivityPub)
	// Deliberately do NOT seed sk.Operators.

	inv := keeper.OrphanBindingsInvariant(f.keeper)
	msg, broken := inv(f.sdkCtx())
	require.True(t, broken, "expected invariant to flag orphan binding")
	require.Contains(t, msg, "no live service.Operator")
	_ = sk // satisfy lint
}

func TestOrphanBindingsInvariant_ClearWhenAllBindingsHaveLiveOperator(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sk := wireServiceKeeper(t, f)

	registerTestPeer(t, f, ms, "mastodon.social")
	op := testAddr(t, f, "op-live")
	st := types.ServiceTypeFederationBridgeActivityPub
	seedBinding(t, f, op, "mastodon.social", st)

	// Seed a live service.Operator.
	sk.Operators[sk.opKey(op, st)] = servicetypes.Operator{
		Address:     op,
		ServiceType: st,
		Status:      servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE,
	}
	// Wire GetOperator to return the seeded record by addrBytes.
	sk.GetOperatorFn = func(_ context.Context, addrBytes []byte, serviceType string) (servicetypes.Operator, bool) {
		s, _ := f.addressCodec.BytesToString(addrBytes)
		o, ok := sk.Operators[s+"/"+serviceType]
		return o, ok
	}

	inv := keeper.OrphanBindingsInvariant(f.keeper)
	_, broken := inv(f.sdkCtx())
	require.False(t, broken, "no orphans expected")
}

func TestBindingsByOperatorIndexInvariant_DetectsMissingReverseEntry(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	_ = wireServiceKeeper(t, f)

	registerTestPeer(t, f, ms, "mastodon.social")
	op := testAddr(t, f, "op-idx")
	st := types.ServiceTypeFederationBridgeActivityPub

	// Write only the primary binding, NOT the reverse index — simulates
	// the fail-soft hook drop scenario.
	binding := types.BridgeBinding{
		Address:      op,
		PeerId:       "mastodon.social",
		Protocol:     "activitypub",
		Endpoint:     "https://x.example.com",
		RegisteredAt: f.sdkCtx().BlockTime().Unix(),
	}
	require.NoError(t, f.keeper.BridgeBindings.Set(f.ctx, collections.Join(op, "mastodon.social"), binding))
	// Skip BridgesByPeer and BindingsByOperator on purpose.

	inv := keeper.BindingsByOperatorIndexInvariant(f.keeper)
	msg, broken := inv(f.sdkCtx())
	require.True(t, broken, "invariant should flag missing reverse-index entry")
	require.Contains(t, msg, "missing reverse-index entry")
	_ = st
}
