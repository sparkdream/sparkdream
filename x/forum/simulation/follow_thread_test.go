package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgFollowThread_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgFollowThread(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgFollowThread returned nil operation")
	}
}
