package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgUnfollowThread_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgUnfollowThread(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgUnfollowThread returned nil operation")
	}
}
