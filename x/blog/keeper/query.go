package keeper

import (
	"sparkdream/x/blog/types"
)

var _ types.QueryServer = Keeper{}
