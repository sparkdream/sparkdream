package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgRejectProposedReply_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgRejectProposedReply(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgRejectProposedReply returned nil operation")
	}
}
