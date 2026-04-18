package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgUnlockThread_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgUnlockThread(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgUnlockThread returned nil operation")
	}
}
