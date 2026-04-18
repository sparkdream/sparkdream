package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgDeletePost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgDeletePost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgDeletePost returned nil operation")
	}
}
