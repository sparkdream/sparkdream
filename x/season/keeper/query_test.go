package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestNewQueryServerImpl(t *testing.T) {
	f := initFixture(t)

	qs := keeper.NewQueryServerImpl(f.keeper)
	require.NotNil(t, qs)

	var _ types.QueryServer = qs
}
