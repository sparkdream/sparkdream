package keeper_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestNewMsgServerImpl(t *testing.T) {
	f := initFixture(t)

	ms := keeper.NewMsgServerImpl(f.keeper)
	if ms == nil {
		t.Fatal("NewMsgServerImpl returned nil")
	}

	var _ types.MsgServer = ms
}
