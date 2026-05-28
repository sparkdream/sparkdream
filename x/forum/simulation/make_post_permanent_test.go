package simulation_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/simulation"
)

func TestSimulateMsgMakePostPermanent_ReturnsOperation(t *testing.T) {
	op := simulation.SimulateMsgMakePostPermanent(nil, nil, keeper.Keeper{}, nil)
	if op == nil {
		t.Fatal("SimulateMsgMakePostPermanent returned nil operation")
	}
}
