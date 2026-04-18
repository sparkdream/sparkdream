package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgDownvotePost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgDownvotePost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgDownvotePost returned nil operation")
	}
}
