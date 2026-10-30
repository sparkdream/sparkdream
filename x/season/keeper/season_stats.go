package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"
)

// Running per-season aggregates for the SeasonStats query (SEA-35). They
// are updated where the events happen (XP grants, member zeroing, quest
// claims, initiative completions), so the query is a single read instead
// of scans over every profile, quest progress and guild.
//
// The current season's counters live in one item (CurrentSeasonStats) so
// the XP-grant path, which runs inside council votes, costs one small read
// and one small write. Season finalisation archives them into
// SeasonStatsCounters under the finished season's number and starts the
// new season at zero.

// getCurrentSeasonStats returns the current season's counters (zero when
// none were recorded yet).
func (k Keeper) getCurrentSeasonStats(ctx context.Context) (types.SeasonStatsCounters, error) {
	c, err := k.CurrentSeasonStats.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return types.SeasonStatsCounters{}, nil
	}
	return c, err
}

// GetSeasonStatsCounters returns the counters of a season: the running
// ones for the current season, the archived ones for a past season, zero
// when none were recorded.
func (k Keeper) GetSeasonStatsCounters(ctx context.Context, season uint64) (types.SeasonStatsCounters, error) {
	if cur, err := k.Season.Get(ctx); err == nil && cur.Number == season {
		c, err := k.getCurrentSeasonStats(ctx)
		c.Season = season
		return c, err
	}
	c, err := k.SeasonStatsCounters.Get(ctx, season)
	if errors.Is(err, collections.ErrNotFound) {
		return types.SeasonStatsCounters{Season: season}, nil
	}
	return c, err
}

// archiveSeasonStats moves the current counters under the finished
// season's number and starts the next season at zero. Called when the
// season number advances.
func (k Keeper) archiveSeasonStats(ctx context.Context, finished, next uint64) error {
	c, err := k.getCurrentSeasonStats(ctx)
	if err != nil {
		return err
	}
	c.Season = finished
	if err := k.SeasonStatsCounters.Set(ctx, finished, c); err != nil {
		return err
	}
	return k.CurrentSeasonStats.Set(ctx, types.SeasonStatsCounters{Season: next})
}

// updateSeasonStats applies fn to the current season's counters.
//
// The read and write of this one fixed-size item are not charged to the
// caller's gas meter: the XP grant runs inside council votes, whose gas
// headroom is small (TestVoteHookGas), and the work is constant per call,
// so leaving it unmetered opens no griefing vector. The write still goes
// to the caller's store, so it commits or rolls back with the caller.
func (k Keeper) updateSeasonStats(ctx context.Context, fn func(c *types.SeasonStatsCounters)) error {
	free := sdk.UnwrapSDKContext(ctx).WithGasMeter(storetypes.NewInfiniteGasMeter())
	c, err := k.getCurrentSeasonStats(free)
	if err != nil {
		return err
	}
	fn(&c)
	return k.CurrentSeasonStats.Set(free, c)
}

// recordXPGrantStats counts a grant of amount XP to a member who held
// prevSeasonXP before it.
func (k Keeper) recordXPGrantStats(ctx context.Context, prevSeasonXP, amount uint64) error {
	if amount == 0 {
		return nil
	}
	return k.updateSeasonStats(ctx, func(c *types.SeasonStatsCounters) {
		c.TotalXpEarned = satAdd(c.TotalXpEarned, amount)
		if prevSeasonXP == 0 {
			c.ActiveMembers++
		}
	})
}

// recordXPRemovedStats removes a member's season XP from the aggregates
// (zeroing).
func (k Keeper) recordXPRemovedStats(ctx context.Context, seasonXP uint64) error {
	if seasonXP == 0 {
		return nil
	}
	return k.updateSeasonStats(ctx, func(c *types.SeasonStatsCounters) {
		c.TotalXpEarned = satSub(c.TotalXpEarned, seasonXP)
		c.ActiveMembers = satSub(c.ActiveMembers, 1)
	})
}

func (k Keeper) recordQuestCompletedStats(ctx context.Context) error {
	return k.updateSeasonStats(ctx, func(c *types.SeasonStatsCounters) { c.QuestsCompleted++ })
}

func (k Keeper) recordInitiativeCompletedStats(ctx context.Context) error {
	return k.updateSeasonStats(ctx, func(c *types.SeasonStatsCounters) { c.InitiativesCompleted++ })
}

func satAdd(a, b uint64) uint64 {
	if a > ^uint64(0)-b {
		return ^uint64(0)
	}
	return a + b
}

func satSub(a, b uint64) uint64 {
	if b > a {
		return 0
	}
	return a - b
}
