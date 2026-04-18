package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgIncreaseBounty_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgIncreaseBounty(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgIncreaseBounty returned nil operation")
	}
}
