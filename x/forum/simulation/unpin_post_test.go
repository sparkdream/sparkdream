package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgUnpinPost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgUnpinPost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgUnpinPost returned nil operation")
	}
}
