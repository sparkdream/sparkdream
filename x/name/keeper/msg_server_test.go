package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestNewMsgServerImpl(t *testing.T) {
	f := initFixture(t)

	ms := keeper.NewMsgServerImpl(f.keeper)
	require.NotNil(t, ms)
	require.Implements(t, (*types.MsgServer)(nil), ms)
}
