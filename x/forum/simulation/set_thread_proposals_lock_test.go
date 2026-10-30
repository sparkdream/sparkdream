package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgSetThreadProposalsLock_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgSetThreadProposalsLock(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgSetThreadProposalsLock returned nil operation")
	}
}
