package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgAppealThreadLock_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgAppealThreadLock(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgAppealThreadLock returned nil operation")
	}
}
