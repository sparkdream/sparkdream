package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgHidePost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgHidePost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgHidePost returned nil operation")
	}
}
