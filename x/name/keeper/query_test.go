package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestNewQueryServerImpl(t *testing.T) {
	f := initFixture(t)

	qs := keeper.NewQueryServerImpl(f.keeper)
	require.NotNil(t, qs)
	require.Implements(t, (*types.QueryServer)(nil), qs)
}
