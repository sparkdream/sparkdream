package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgMoveThread_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgMoveThread(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgMoveThread returned nil operation")
	}
}
