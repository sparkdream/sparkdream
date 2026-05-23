package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	servicetypes "sparkdream/x/service/types"
)

// ---------------------------------------------------------------------------
// Shared test infrastructure (mockServiceKeeper, seedBinding,
// fixture.sdkCtx/hookCtx helpers, errorContainsAny).
//
// These helpers were originally introduced for the federation→service
// migration follow-up suite. They live in this file because the hook
// tests below were the first consumer and they fit naturally with the
// other "things tests need to wire to exercise federation against a
// fake x/service" code. Several sibling _test.go files in this package
// (invariants_test.go, msg_server_prune_orphan_bindings_test.go,
// msg_server_update_peer_controller_test.go) reuse them via the
// shared keeper_test package.
// ---------------------------------------------------------------------------

// mockServiceKeeper implements federationtypes.ServiceKeeper for tests.
// Tracks all calls so assertions can check what federation handed off
// to x/service.
type mockServiceKeeper struct {
	// Configurable behavior.
	RegisterOperatorFn func(ctx context.Context, creator, serviceType, controller string, bondAmount math.Int, metadata []byte, source int) (servicetypes.Operator, error)
	TopUpBondFn        func(ctx context.Context, opBytes []byte, serviceType string, additionalBondAmount math.Int) error
	OpenSystemReportFn func(ctx context.Context, callerModuleAddr sdk.AccAddress, operatorAddr sdk.AccAddress, serviceType string, slashBps uint32, evidenceURI string, dedupeKey []byte) (uint64, bool, error)
	GetOperatorFn      func(ctx context.Context, addrBytes []byte, serviceType string) (servicetypes.Operator, bool)
	HasSlashedRecordFn func(ctx context.Context, addrBytes []byte, serviceType string) (bool, error)
	GetServiceTypeFn   func(ctx context.Context, serviceType string) (servicetypes.ServiceTypeConfig, bool)

	// Call recorders.
	RegisterCalls []mockSKRegister
	TopUpCalls    []mockSKTopUp
	ReportCalls   []mockSKReport

	// State.
	Operators map[string]servicetypes.Operator // key: addr/service_type
	NextRptID uint64
}

type mockSKRegister struct {
	Creator     string
	ServiceType string
	Controller  string
	BondAmount  math.Int
	Metadata    []byte
}

type mockSKTopUp struct {
	OpAddr      string
	ServiceType string
	Additional  math.Int
}

type mockSKReport struct {
	Caller      sdk.AccAddress
	Operator    sdk.AccAddress
	ServiceType string
	SlashBps    uint32
	EvidenceURI string
	DedupeKey   []byte
}

func newMockServiceKeeper() *mockServiceKeeper {
	return &mockServiceKeeper{
		Operators: map[string]servicetypes.Operator{},
	}
}

func (m *mockServiceKeeper) opKey(addr, st string) string { return addr + "/" + st }

func (m *mockServiceKeeper) RegisterOperator(ctx context.Context, creator, serviceType, controller string, bondAmount math.Int, metadata []byte, source int) (servicetypes.Operator, error) {
	m.RegisterCalls = append(m.RegisterCalls, mockSKRegister{creator, serviceType, controller, bondAmount, metadata})
	if m.RegisterOperatorFn != nil {
		return m.RegisterOperatorFn(ctx, creator, serviceType, controller, bondAmount, metadata, source)
	}
	op := servicetypes.Operator{
		Address:     creator,
		ServiceType: serviceType,
		Controller:  controller,
		BondAmount:  bondAmount,
		Status:      servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE,
	}
	m.Operators[m.opKey(creator, serviceType)] = op
	return op, nil
}

func (m *mockServiceKeeper) TopUpBond(ctx context.Context, opBytes []byte, serviceType string, additionalBondAmount math.Int) error {
	m.TopUpCalls = append(m.TopUpCalls, mockSKTopUp{string(opBytes), serviceType, additionalBondAmount})
	if m.TopUpBondFn != nil {
		return m.TopUpBondFn(ctx, opBytes, serviceType, additionalBondAmount)
	}
	return nil
}

func (m *mockServiceKeeper) OpenSystemReport(ctx context.Context, callerModuleAddr, operatorAddr sdk.AccAddress, serviceType string, slashBps uint32, evidenceURI string, dedupeKey []byte) (uint64, bool, error) {
	m.ReportCalls = append(m.ReportCalls, mockSKReport{callerModuleAddr, operatorAddr, serviceType, slashBps, evidenceURI, dedupeKey})
	if m.OpenSystemReportFn != nil {
		return m.OpenSystemReportFn(ctx, callerModuleAddr, operatorAddr, serviceType, slashBps, evidenceURI, dedupeKey)
	}
	m.NextRptID++
	return m.NextRptID, false, nil
}

func (m *mockServiceKeeper) GetOperator(ctx context.Context, addrBytes []byte, serviceType string) (servicetypes.Operator, bool) {
	if m.GetOperatorFn != nil {
		return m.GetOperatorFn(ctx, addrBytes, serviceType)
	}
	// Default: convert addrBytes back to bech32 via the fixture's codec
	// is awkward here; instead, callers can seed Operators map directly.
	for k, op := range m.Operators {
		if op.ServiceType == serviceType && k == op.Address+"/"+serviceType {
			// Compare by reconstructed addr — caller seeded with the
			// bech32 string so reverse-resolve isn't needed here.
			return op, true
		}
	}
	return servicetypes.Operator{}, false
}

func (m *mockServiceKeeper) HasSlashedRecord(ctx context.Context, addrBytes []byte, serviceType string) (bool, error) {
	if m.HasSlashedRecordFn != nil {
		return m.HasSlashedRecordFn(ctx, addrBytes, serviceType)
	}
	return false, nil
}

func (m *mockServiceKeeper) GetServiceTypeConfig(ctx context.Context, serviceType string) (servicetypes.ServiceTypeConfig, bool) {
	if m.GetServiceTypeFn != nil {
		return m.GetServiceTypeFn(ctx, serviceType)
	}
	return servicetypes.ServiceTypeConfig{ServiceType: serviceType, Enabled: true}, true
}

// wireServiceKeeper attaches the mock service keeper to the fixture's
// federation keeper. Returns the mock for assertion access.
func wireServiceKeeper(t *testing.T, f *fixture) *mockServiceKeeper {
	t.Helper()
	sk := newMockServiceKeeper()
	f.keeper.SetServiceKeeper(sk)
	return sk
}

// seedBinding writes a BridgeBinding + both indexes directly (skipping
// MsgRegisterBridge). Used to set up the state hooks should manipulate.
func seedBinding(t *testing.T, f *fixture, operator, peerID, serviceType string) {
	t.Helper()
	bindingKey := collections.Join(operator, peerID)
	binding := types.BridgeBinding{
		Address:      operator,
		PeerId:       peerID,
		Protocol:     "activitypub",
		Endpoint:     "https://" + operator + ".example.com",
		RegisteredAt: f.sdkCtx().BlockTime().Unix(),
	}
	require.NoError(t, f.keeper.BridgeBindings.Set(f.ctx, bindingKey, binding))
	require.NoError(t, f.keeper.BridgesByPeer.Set(f.ctx, collections.Join(peerID, operator)))
	require.NoError(t, f.keeper.BindingsByOperator.Set(f.ctx, collections.Join3(serviceType, operator, peerID)))
}

// sdkCtx returns the underlying sdk.Context from the fixture.
func (f *fixture) sdkCtx() sdk.Context {
	return sdk.UnwrapSDKContext(f.ctx)
}

// hookCtx returns the fixture's context.Context, suitable for hook
// callbacks which expect the unwrapped form.
func (f *fixture) hookCtx() context.Context { return f.ctx }

// errorContainsAny returns true if err's message contains any of the
// given substrings. Lets tests assert against wrapped errors without
// pinning to exact registration codes.
func errorContainsAny(err error, subs ...string) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, sub := range subs {
		if containsSubstring(s, sub) {
			return true
		}
	}
	return false
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	// Simple linear search; avoids pulling in strings for a single use.
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// FederationServiceHooks: all 4 callbacks
// ---------------------------------------------------------------------------

func TestFederationServiceHooks_AfterOperatorDissolved_PrunesAllBindings(t *testing.T) {
	f := initFixture(t)
	hooks := keeper.NewFederationServiceHooks(f.keeper)

	const peerA = "mastodon.social"
	const peerB = "fosstodon.org"
	st := types.ServiceTypeFederationBridgeActivityPub
	op := testAddr(t, f, "op-dissolve")

	// Two bindings for the same operator under one service_type
	// (Decision 1a: multi-binding allowed).
	seedBinding(t, f, op, peerA, st)
	seedBinding(t, f, op, peerB, st)

	opBytes, _ := f.addressCodec.StringToBytes(op)
	hooks.AfterOperatorDissolved(f.hookCtx(), opBytes, st)

	// Both bindings should be gone, plus their reverse-index entries.
	_, errA := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(op, peerA))
	require.Error(t, errA)
	_, errB := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(op, peerB))
	require.Error(t, errB)
	has, _ := f.keeper.BindingsByOperator.Has(f.ctx, collections.Join3(st, op, peerA))
	require.False(t, has)
	has, _ = f.keeper.BindingsByOperator.Has(f.ctx, collections.Join3(st, op, peerB))
	require.False(t, has)
}

func TestFederationServiceHooks_AfterOperatorRetired_PrunesSameWayAsDissolved(t *testing.T) {
	f := initFixture(t)
	hooks := keeper.NewFederationServiceHooks(f.keeper)

	const peer = "mastodon.social"
	st := types.ServiceTypeFederationBridgeActivityPub
	op := testAddr(t, f, "op-retire")
	seedBinding(t, f, op, peer, st)

	opBytes, _ := f.addressCodec.StringToBytes(op)
	hooks.AfterOperatorRetired(f.hookCtx(), opBytes, st)

	_, err := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(op, peer))
	require.Error(t, err)
}

func TestFederationServiceHooks_AfterOperatorUnderfunded_SetsSuspended(t *testing.T) {
	f := initFixture(t)
	hooks := keeper.NewFederationServiceHooks(f.keeper)

	const peer = "mastodon.social"
	st := types.ServiceTypeFederationBridgeActivityPub
	op := testAddr(t, f, "op-underfund")
	seedBinding(t, f, op, peer, st)

	opBytes, _ := f.addressCodec.StringToBytes(op)
	hooks.AfterOperatorUnderfunded(f.hookCtx(), opBytes, st)

	binding, err := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(op, peer))
	require.NoError(t, err)
	require.True(t, binding.Suspended)
}

func TestFederationServiceHooks_AfterOperatorReFunded_ClearsSuspended(t *testing.T) {
	f := initFixture(t)
	hooks := keeper.NewFederationServiceHooks(f.keeper)

	const peer = "mastodon.social"
	st := types.ServiceTypeFederationBridgeActivityPub
	op := testAddr(t, f, "op-refund")
	seedBinding(t, f, op, peer, st)
	// Pre-suspend.
	binding, _ := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(op, peer))
	binding.Suspended = true
	require.NoError(t, f.keeper.BridgeBindings.Set(f.ctx, collections.Join(op, peer), binding))

	opBytes, _ := f.addressCodec.StringToBytes(op)
	hooks.AfterOperatorReFunded(f.hookCtx(), opBytes, st)

	binding, err := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(op, peer))
	require.NoError(t, err)
	require.False(t, binding.Suspended)
}

// Resilience: a hook called against an operator with no bindings must
// be a clean no-op, not a crash. Together with the recoverHookPanic
// defer-recover pattern (verified by code review — it's a small
// defensive wrapper), this property is what keeps a federation bug
// from blocking service slashes.
func TestFederationServiceHooks_Resilient_NoOpOnEmptyState(t *testing.T) {
	f := initFixture(t)
	hooks := keeper.NewFederationServiceHooks(f.keeper)
	st := types.ServiceTypeFederationBridgeActivityPub
	op := testAddr(t, f, "op-empty")
	opBytes, _ := f.addressCodec.StringToBytes(op)

	// No seeded bindings — each hook must return without panicking.
	require.NotPanics(t, func() { hooks.AfterOperatorDissolved(f.hookCtx(), opBytes, st) })
	require.NotPanics(t, func() { hooks.AfterOperatorRetired(f.hookCtx(), opBytes, st) })
	require.NotPanics(t, func() { hooks.AfterOperatorUnderfunded(f.hookCtx(), opBytes, st) })
	require.NotPanics(t, func() { hooks.AfterOperatorReFunded(f.hookCtx(), opBytes, st) })
}

// Resilience: AfterOperatorUnderfunded must skip (not panic on) a
// reverse-index entry whose primary binding has been manually deleted.
// This is the "orphan reverse-index entry" case the hook documents as
// a continue-don't-panic path.
func TestFederationServiceHooks_SkipsDanglingReverseIndexEntry(t *testing.T) {
	f := initFixture(t)
	hooks := keeper.NewFederationServiceHooks(f.keeper)
	st := types.ServiceTypeFederationBridgeActivityPub
	op := testAddr(t, f, "op-dangling")

	// Write only the reverse index, no primary binding.
	require.NoError(t, f.keeper.BindingsByOperator.Set(f.ctx, collections.Join3(st, op, "peer-1")))

	opBytes, _ := f.addressCodec.StringToBytes(op)
	require.NotPanics(t, func() {
		hooks.AfterOperatorUnderfunded(f.hookCtx(), opBytes, st)
	})
}
