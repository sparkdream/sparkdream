package keeper_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestNewQueryServerImpl(t *testing.T) {
	f := initFixture(t)

	qs := keeper.NewQueryServerImpl(f.keeper)
	if qs == nil {
		t.Fatal("NewQueryServerImpl returned nil")
	}

	var _ types.QueryServer = qs
}
