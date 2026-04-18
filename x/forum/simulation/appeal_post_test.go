package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgAppealPost_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgAppealPost(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgAppealPost returned nil operation")
	}
}
