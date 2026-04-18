package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgFreezeThread_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgFreezeThread(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgFreezeThread returned nil operation")
	}
}
