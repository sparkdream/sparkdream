package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgMarkAcceptedReply_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgMarkAcceptedReply(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgMarkAcceptedReply returned nil operation")
	}
}
