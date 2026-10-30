package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveShareByAddress(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, "phoenix", 10))
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, "aurora", 20))

	require.NoError(t, f.keeper.RemoveShareByAddress(f.ctx, "phoenix"))
	has, err := f.keeper.Share.Has(f.ctx, "phoenix")
	require.NoError(t, err)
	require.False(t, has)

	// Removing an address without a share is a no-op.
	require.NoError(t, f.keeper.RemoveShareByAddress(f.ctx, "zenith"))

	has, err = f.keeper.Share.Has(f.ctx, "aurora")
	require.NoError(t, err)
	require.True(t, has)
}
