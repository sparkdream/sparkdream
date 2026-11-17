package keeper_test

import (
	"sparkdream/x/storage/keeper"
	"sparkdream/x/storage/types"
)

func keeperQuery(f *fixture) types.QueryServer { return keeper.NewQueryServerImpl(f.keeper) }
