package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgDisputePin_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgDisputePin(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgDisputePin returned nil operation")
	}
}
