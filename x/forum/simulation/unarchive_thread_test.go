package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgUnarchiveThread_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgUnarchiveThread(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgUnarchiveThread returned nil operation")
	}
}
