package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgCreateBounty_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgCreateBounty(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgCreateBounty returned nil operation")
	}
}
