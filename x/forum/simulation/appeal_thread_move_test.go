package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgAppealThreadMove_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgAppealThreadMove(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgAppealThreadMove returned nil operation")
	}
}
