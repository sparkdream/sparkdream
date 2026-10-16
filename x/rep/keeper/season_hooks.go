package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// notifySeason runs one x/season gamification hook. The originating state
// write has already happened, so a hook failure is logged and swallowed:
// XP bookkeeping must never roll back a membership, initiative or jury
// outcome. No-op when x/season is not wired (standalone tests).
func (k Keeper) notifySeason(ctx context.Context, hook string, fn func(sk types.SeasonKeeper) error) {
	if k.late.seasonKeeper == nil {
		return
	}
	if err := fn(k.late.seasonKeeper); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("x/season hook failed (non-fatal)", "hook", hook, "error", err)
	}
}
