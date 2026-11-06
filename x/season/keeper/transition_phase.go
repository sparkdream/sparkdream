package keeper

import (
	"context"
	"errors"
	"fmt"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// repMemberCountPage is the page size countRepMembers walks x/rep's member
// set with when a transition phase starts.
const repMemberCountPage = 1000

// phaseIsMaintenance reports whether a transition phase runs in maintenance
// mode: the reputation and DREAM the snapshot records must stop moving from
// SNAPSHOT through RESET_XP (SEA-16). Genesis Validate applies the same
// rule to an imported flag (SEA-203).
func phaseIsMaintenance(phase types.TransitionPhase) bool {
	return phase.IsMaintenance()
}

// beginTransitionPhase moves the transition to phase: the cursor is reset,
// the maintenance flag follows the phase, total_count is set, and the live
// season's status is synced (MAINTENANCE while the flag is on, ENDING again
// when it goes off). It does not persist state; the caller does.
//
// total_count is informational, for the SeasonTransitionState query: a phase
// ends when its walk is exhausted, never when processed_count reaches it.
// Counting walks the phase's whole item set (x/rep's member set for
// RESET_REPUTATION), which is block-time only when the BeginBlocker starts
// a phase (count = true) but gas inside a tx: MsgRetrySeasonTransition and
// MsgSkipTransitionPhase pass count = false so the recovery tools stay
// usable from the committee path at any membership (SEA-201). Without a
// count, a restart of the same phase keeps the previous total_count and a
// new phase gets 0 = unknown.
func (k Keeper) beginTransitionPhase(ctx context.Context, state *types.SeasonTransitionState, phase types.TransitionPhase, count bool) error {
	if phase != state.Phase {
		state.TotalCount = 0
	}
	state.Phase = phase
	state.ProcessedCount = 0
	state.LastProcessed = ""
	state.MaintenanceMode = phaseIsMaintenance(phase)

	season, err := k.Season.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// No live season (fixtures only): nothing to count or sync.
			state.TotalCount = 0
			return nil
		}
		return err
	}
	if count {
		total, err := k.countTransitionPhaseItems(ctx, phase, season.Number)
		if err != nil {
			return err
		}
		state.TotalCount = total
	}
	return k.syncSeasonStatus(ctx, season, state.MaintenanceMode)
}

// syncSeasonStatus makes the live season's status reflect the transition:
// MAINTENANCE while a maintenance phase runs, ENDING once the first
// non-maintenance phase begins. A status that is neither (an aborted or
// fixture season) is left alone.
func (k Keeper) syncSeasonStatus(ctx context.Context, season types.Season, maintenance bool) error {
	var want types.SeasonStatus
	var event string
	switch {
	case maintenance:
		want = types.SeasonStatus_SEASON_STATUS_MAINTENANCE
		event = "maintenance_mode_started"
	case season.Status == types.SeasonStatus_SEASON_STATUS_MAINTENANCE:
		want = types.SeasonStatus_SEASON_STATUS_ENDING
		event = "maintenance_mode_ended"
	default:
		return nil
	}
	if season.Status == want {
		return nil
	}
	season.Status = want
	if err := k.Season.Set(ctx, season); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(event,
		sdk.NewAttribute("season_number", fmt.Sprintf("%d", season.Number)),
		sdk.NewAttribute("status", want.String()),
	))
	return nil
}

// countTransitionPhaseItems returns the number of items a phase will walk:
// the season's nominations for RETRO_REWARDS and RETURN_NOMINATION_STAKES,
// season profiles for the member phases (x/rep's member set for
// RESET_REPUTATION, which walks it), quest progress records for CLEANUP.
func (k Keeper) countTransitionPhaseItems(ctx context.Context, phase types.TransitionPhase, seasonNumber uint64) (uint64, error) {
	switch phase {
	case types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS,
		types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES:
		return k.countSeasonNominations(ctx, seasonNumber)
	case types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
		types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION,
		types.TransitionPhase_TRANSITION_PHASE_RESET_XP,
		types.TransitionPhase_TRANSITION_PHASE_TITLES:
		return countKeys[string, types.MemberProfile](ctx, k.MemberProfile)
	case types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION:
		if k.late.repKeeper != nil {
			return k.countRepMembers(ctx)
		}
		return countKeys[string, types.MemberProfile](ctx, k.MemberProfile)
	case types.TransitionPhase_TRANSITION_PHASE_CLEANUP:
		return countKeys[string, types.MemberQuestProgress](ctx, k.MemberQuestProgress)
	default:
		return 0, nil
	}
}

// keyIterable is the part of collections.Map / IndexedMap countKeys needs.
type keyIterable[K, V any] interface {
	Iterate(ctx context.Context, ranger collections.Ranger[K]) (collections.Iterator[K, V], error)
}

// countKeys counts a collection's entries without decoding values.
func countKeys[K, V any](ctx context.Context, c keyIterable[K, V]) (uint64, error) {
	iter, err := c.Iterate(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer iter.Close()
	var n uint64
	for ; iter.Valid(); iter.Next() {
		n++
	}
	return n, nil
}

// countSeasonNominations counts the (season, id) index entries of a season.
func (k Keeper) countSeasonNominations(ctx context.Context, seasonNumber uint64) (uint64, error) {
	var n uint64
	err := k.NominationsBySeason.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](seasonNumber),
		func(_ collections.Pair[uint64, uint64]) (bool, error) {
			n++
			return false, nil
		})
	return n, err
}

// countRepMembers pages x/rep's member set the way RESET_REPUTATION walks it.
func (k Keeper) countRepMembers(ctx context.Context) (uint64, error) {
	var n uint64
	after := ""
	for {
		members, done, err := k.late.repKeeper.MembersAfter(ctx, after, repMemberCountPage)
		if err != nil {
			return 0, err
		}
		n += uint64(len(members))
		if done || len(members) == 0 {
			return n, nil
		}
		after = members[len(members)-1]
	}
}

// pruneSeasonHistory drops completed-season records older than retention
// seasons before endingSeason (the one the CLEANUP phase is closing), so
// that once it is archived the history holds at most retention records.
func (k Keeper) pruneSeasonHistory(ctx context.Context, endingSeason uint64, retention uint64) error {
	if retention == 0 {
		return nil
	}
	var old []uint64
	if err := k.SeasonHistory.Walk(ctx, nil, func(number uint64, _ types.Season) (bool, error) {
		if number+retention <= endingSeason {
			old = append(old, number)
			return false, nil
		}
		return true, nil // ascending: the rest is younger
	}); err != nil {
		return err
	}
	for _, number := range old {
		if err := k.SeasonHistory.Remove(ctx, number); err != nil {
			return err
		}
	}
	return nil
}
