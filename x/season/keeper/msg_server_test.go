package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestNewMsgServerImpl(t *testing.T) {
	f := initFixture(t)

	ms := keeper.NewMsgServerImpl(f.keeper)
	require.NotNil(t, ms)

	var _ types.MsgServer = ms
}
