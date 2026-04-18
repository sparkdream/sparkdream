package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgLockThread_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgLockThread(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgLockThread returned nil operation")
	}
}
