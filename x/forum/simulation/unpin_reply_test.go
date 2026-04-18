package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgUnpinReply_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgUnpinReply(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgUnpinReply returned nil operation")
	}
}
