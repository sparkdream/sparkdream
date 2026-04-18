package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgAwardBounty_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgAwardBounty(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgAwardBounty returned nil operation")
	}
}
