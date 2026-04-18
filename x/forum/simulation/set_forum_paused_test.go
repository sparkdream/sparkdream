package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgSetForumPaused_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgSetForumPaused(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgSetForumPaused returned nil operation")
	}
}
