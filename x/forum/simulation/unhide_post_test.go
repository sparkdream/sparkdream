package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgUnhidePost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgUnhidePost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgUnhidePost returned nil operation")
	}
}
