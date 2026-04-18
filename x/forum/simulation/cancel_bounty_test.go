package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgCancelBounty_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgCancelBounty(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgCancelBounty returned nil operation")
	}
}
