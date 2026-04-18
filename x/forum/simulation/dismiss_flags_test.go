package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgDismissFlags_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgDismissFlags(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgDismissFlags returned nil operation")
	}
}
