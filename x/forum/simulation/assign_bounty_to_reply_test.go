package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgAssignBountyToReply_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgAssignBountyToReply(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgAssignBountyToReply returned nil operation")
	}
}
