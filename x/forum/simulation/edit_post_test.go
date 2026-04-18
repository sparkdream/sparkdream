package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgEditPost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgEditPost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgEditPost returned nil operation")
	}
}
