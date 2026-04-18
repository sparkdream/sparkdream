package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgPinPost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgPinPost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgPinPost returned nil operation")
	}
}
