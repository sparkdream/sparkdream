package keeper_test

import (
	"testing"

	repkeeper "sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// The chain-wide review gate keys on how much DREAM a completion CREATES, not
// on whether the project is budget-backed. Permissionless initiatives mint
// against a self-declared budget with no treasury behind them, so the
// funded/unfunded axis gets the risk ordering backwards.
//
// These tests use a low threshold rather than a large budget so they exercise
// the gate without also exercising the tier budget ceilings.

func setGateThreshold(t *testing.T, f *fixture, threshold int64) types.Params {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.ReviewRequiredAboveBudget = math.NewInt(threshold)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	return params
}

func TestGateBlocksCompletionAboveThresholdWithNoProjectPolicy(t *testing.T) {
	f := initFixture(t)
	setGateThreshold(t, f, 50)

	// Budget 100 > threshold 50, and the project declares no policy at all.
	initID := buildCompletableInitiative(t, f.keeper, f.ctx, math.NewInt(100), "_gated", true)

	can, err := f.keeper.CanCompleteInitiative(f.ctx, initID)
	require.NoError(t, err)
	require.False(t, can, "a project with no verification policy must not dodge the chain-wide gate")

	require.Error(t, f.keeper.CompleteInitiative(f.ctx, initID))
}

func TestGateExemptsCompletionAtOrBelowThreshold(t *testing.T) {
	f := initFixture(t)
	setGateThreshold(t, f, 100)

	// Budget 100 is NOT greater than threshold 100 — the comparison is strict,
	// so the tier ceiling itself stays exempt rather than falling just inside.
	initID := buildCompletableInitiative(t, f.keeper, f.ctx, math.NewInt(100), "_exempt", true)

	can, err := f.keeper.CanCompleteInitiative(f.ctx, initID)
	require.NoError(t, err)
	require.True(t, can, "work at or under the threshold completes on conviction as before")
	require.NoError(t, f.keeper.CompleteInitiative(f.ctx, initID))
}

func TestGateTakesTheMaxOfPolicyAndThreshold(t *testing.T) {
	f := initFixture(t)
	params := setGateThreshold(t, f, 50)

	init := types.Initiative{Id: 1, Budget: repkeeper.PtrInt(math.NewInt(100)), RequiredVerifiers: 3}
	proj := types.Project{Id: 1, VerificationPolicy: &types.VerificationPolicy{MinVerifierCount: 3}}
	require.Equal(t, uint32(3), repkeeper.RequiredVerifiersFor(params, init, proj),
		"a project demanding more than the threshold keeps its own higher bar")

	// And a project demanding nothing still gets the threshold's single verdict.
	initNoPolicy := types.Initiative{Id: 2, Budget: repkeeper.PtrInt(math.NewInt(100))}
	require.Equal(t, uint32(1), repkeeper.RequiredVerifiersFor(params, initNoPolicy, types.Project{Id: 2}))

	// Below the threshold with no policy: no gate.
	initSmall := types.Initiative{Id: 3, Budget: repkeeper.PtrInt(math.NewInt(10))}
	require.Equal(t, uint32(0), repkeeper.RequiredVerifiersFor(params, initSmall, types.Project{Id: 3}))
}

func TestGateThresholdIsReadLiveSoPolicyCannotDodgeIt(t *testing.T) {
	// The project policy is snapshotted because its setter is the party the
	// gate constrains. The chain-wide threshold is read live because its setter
	// (committee/gov) is not — otherwise a committee raising the threshold to
	// respond to a farm in progress could not touch anything already submitted.
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)

	// Snapshot says no review required (submitted while ungated).
	init := types.Initiative{Id: 1, Budget: repkeeper.PtrInt(math.NewInt(100)), RequiredVerifiers: 0}
	proj := types.Project{Id: 1}

	params.ReviewRequiredAboveBudget = math.NewInt(1000) // above the budget
	require.Equal(t, uint32(0), repkeeper.RequiredVerifiersFor(params, init, proj))

	params.ReviewRequiredAboveBudget = math.NewInt(50) // lowered under it
	require.Equal(t, uint32(1), repkeeper.RequiredVerifiersFor(params, init, proj),
		"lowering the threshold must catch work already submitted")
}

func TestSweepAdoptsInitiativesThatComeUnderTheGateLate(t *testing.T) {
	// The wedge this exists to prevent: an initiative submitted while ungated
	// has ReviewDeadline 0. Once the gate applies it can no longer complete,
	// and without adoption the escalation sweep would skip it forever for want
	// of a deadline — so it could neither pass, bounce, nor abandon.
	f := initFixture(t)
	setGateThreshold(t, f, 1000) // above the budget: ungated at submission

	initID := buildCompletableInitiative(t, f.keeper, f.ctx, math.NewInt(100), "_late", true)
	before, err := f.keeper.GetInitiative(f.ctx, initID)
	require.NoError(t, err)
	require.Zero(t, before.ReviewDeadline, "ungated work opens no review window")

	// The threshold drops under the budget.
	setGateThreshold(t, f, 50)

	can, err := f.keeper.CanCompleteInitiative(f.ctx, initID)
	require.NoError(t, err)
	require.False(t, can, "now gated, so it cannot complete")

	require.NoError(t, f.keeper.SweepReviewDeadlines(f.ctx))

	adopted, err := f.keeper.GetInitiative(f.ctx, initID)
	require.NoError(t, err)
	require.NotZero(t, adopted.ReviewDeadline,
		"the sweep must open a window rather than leaving it invisible and unable to complete")
	require.Greater(t, adopted.ReviewDeadline, f.ctx.BlockHeight(),
		"a full window under the new rules, not one that already expired")
}

func TestSweepDoesNotAdoptUngatedInitiatives(t *testing.T) {
	f := initFixture(t)
	setGateThreshold(t, f, 1000) // budget 100 stays under it

	initID := buildCompletableInitiative(t, f.keeper, f.ctx, math.NewInt(100), "_ungated", true)
	require.NoError(t, f.keeper.SweepReviewDeadlines(f.ctx))

	after, err := f.keeper.GetInitiative(f.ctx, initID)
	require.NoError(t, err)
	require.Zero(t, after.ReviewDeadline,
		"work the gate does not apply to must not be pulled into a review window")
}
