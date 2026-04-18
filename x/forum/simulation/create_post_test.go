package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgCreatePost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgCreatePost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgCreatePost returned nil operation")
	}
}
