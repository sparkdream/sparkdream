//go:build !mainnet && !testnet && !devnet

package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

// Sanity check that the testparams build chooses the short
// DefaultHiddenExpiration window. If this fails, the build tag wiring
// regressed and shell e2e tests would silently wait 7 days for the
// EndBlocker hide-finalization hook to fire.
func TestDefaultHiddenExpiration_TestparamsIsShort(t *testing.T) {
	require.Equal(t, int64(15), types.DefaultHiddenExpiration,
		"testparams DefaultHiddenExpiration must be 15s so e2e shell tests can observe hide finalization within a single run")
}
