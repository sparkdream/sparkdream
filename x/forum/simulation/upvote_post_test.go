package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgUpvotePost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgUpvotePost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgUpvotePost returned nil operation")
	}
}
