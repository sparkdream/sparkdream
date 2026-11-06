package types_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// arbiter_resolution_window may not exceed x/rep's MaxAnonymousActionWindow:
// arbiter votes are one-per-content anonymous actions against the live
// member tree, so the round must close before a replaced ZK key activates.
// The bound is per network build and off in the test build; run with
// -tags mainnet|testnet|devnet to exercise it.
func TestArbiterWindowWithinAnonymousActionWindow(t *testing.T) {
	bound := reptypes.MaxAnonymousActionWindow
	if bound == 0 {
		t.Skip("no anonymous action window bound in this build")
	}
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	require.LessOrEqual(t, p.ArbiterResolutionWindow, bound)

	p.ArbiterResolutionWindow = bound
	require.NoError(t, p.Validate())
	p.ArbiterResolutionWindow = bound + time.Second
	require.ErrorContains(t, p.Validate(), "arbiter_resolution_window must be at most")
}
