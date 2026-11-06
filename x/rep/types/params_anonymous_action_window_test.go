package types_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// The ZK key activation delay must cover MaxAnonymousActionWindow (the
// federation arbiter vote window ceiling), or a member who replaces their
// key could vote twice in one round. The bound is per network build and off
// in the test build; run with -tags mainnet|testnet|devnet to exercise it.
func TestZkKeyDelayCoversAnonymousActionWindow(t *testing.T) {
	if types.MaxAnonymousActionWindow == 0 {
		t.Skip("no anonymous action window bound in this build")
	}
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	require.GreaterOrEqual(t, p.ZkKeyActivationDelayDuration(), types.MaxAnonymousActionWindow)

	// One epoch of delay (the old floor) no longer validates.
	p.ZkKeyActivationDelay = 1
	p.EpochBlocks = 1
	require.ErrorContains(t, p.Validate(), "anonymous action window")

	// The boundary: exactly the window is enough.
	p.EpochBlocks = 10
	p.ZkKeyActivationDelay = int64(types.MaxAnonymousActionWindow/time.Second) / 60
	require.Equal(t, types.MaxAnonymousActionWindow, p.ZkKeyActivationDelayDuration())
	require.NoError(t, p.Validate())
}
