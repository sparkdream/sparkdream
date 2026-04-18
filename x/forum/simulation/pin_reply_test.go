package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgPinReply_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgPinReply(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgPinReply returned nil operation")
	}
}
