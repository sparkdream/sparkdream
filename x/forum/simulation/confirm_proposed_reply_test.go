package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgConfirmProposedReply_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgConfirmProposedReply(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgConfirmProposedReply returned nil operation")
	}
}
