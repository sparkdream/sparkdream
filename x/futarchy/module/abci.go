package futarchy

import (
	"context"

	"sparkdream/x/futarchy/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker resolves expired markets. It never returns an error: a failing
// market is logged and retried next block (see Keeper.ResolveExpiredMarkets),
// because an EndBlock error is a consensus failure that would halt the chain.
func EndBlocker(ctx context.Context, k keeper.Keeper) error {
	k.ResolveExpiredMarkets(sdk.UnwrapSDKContext(ctx))
	return nil
}
