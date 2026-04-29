package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const maxPrunePerBlock = 100

// EndBlocker prunes expired sessions using the SessionsByExpiration index.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	blockTime := ctx.BlockTime()
	pruned := 0

	// Range scan: all entries with expiration <= blockTime.Unix()
	// The triple key is (expiration_unix, granter, grantee).
	// We iterate from the beginning up to (blockTime.Unix()+1, "", "") exclusive.
	rng := new(collections.Range[collections.Triple[int64, string, string]]).
		EndExclusive(collections.Join3(blockTime.Unix()+1, "", ""))

	err := k.SessionsByExpiration.Walk(ctx, rng, func(key collections.Triple[int64, string, string]) (bool, error) {
		if pruned >= maxPrunePerBlock {
			return true, nil // stop — remaining cleaned up next block
		}

		granter := key.K2()
		grantee := key.K3()

		// Delete session and all indexes
		sessionKey := collections.Join(granter, grantee)
		session, err := k.Sessions.Get(ctx, sessionKey)
		if err != nil {
			// Index is stale — log so operators can detect drift, then remove it.
			ctx.Logger().Debug("session pruner: stale expiration index entry",
				"granter", granter, "grantee", grantee, "expiration", key.K1(), "err", err)
			_ = k.SessionsByExpiration.Remove(ctx, key)
			pruned++
			return false, nil
		}

		if err := k.Sessions.Remove(ctx, sessionKey); err != nil {
			return true, err
		}
		if err := k.SessionsByGranter.Remove(ctx, collections.Join(granter, grantee)); err != nil {
			return true, err
		}
		if err := k.SessionsByGrantee.Remove(ctx, collections.Join(grantee, granter)); err != nil {
			return true, err
		}
		if err := k.SessionsByExpiration.Remove(ctx, key); err != nil {
			return true, err
		}

		ctx.EventManager().EmitEvent(sdk.NewEvent(
			"session_expired",
			sdk.NewAttribute("granter", granter),
			sdk.NewAttribute("grantee", grantee),
			sdk.NewAttribute("exec_count", fmt.Sprintf("%d", session.ExecCount)),
			sdk.NewAttribute("spent", session.Spent.String()),
		))

		pruned++
		return false, nil
	})

	return err
}
