package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestNewQueryServerImpl(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewQueryServerImpl(f.keeper)
	require.NotNil(t, srv)

	var _ types.QueryServer = srv
}
