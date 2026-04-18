package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgFlagPost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgFlagPost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgFlagPost returned nil operation")
	}
}
