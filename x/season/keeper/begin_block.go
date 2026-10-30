package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker handles automatic season transitions and expired moderation auto-resolution.
// Called at the beginning of each block.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	// Process expired display name moderations (runs every block, independent of season transitions)
	k.processExpiredModerations(ctx)
	// Time out appeals nobody resolved (SEA-23).
	k.processExpiredAppeals(ctx)

	// Bounded rolling pruning of history collections and expired guild
	// invites (SEA-11).
	k.pruneHistory(ctx)

	// Retry failed retro-PGF payouts once per epoch (SEA-21).
	k.retryRetroPayouts(ctx)

	// Retry a failed x/rep seasonal pool initialisation (SEA-22).
	k.retryPendingSeasonalPoolInit(ctx)

	// Get current season
	season, err := k.Season.Get(ctx)
	if err != nil {
		// No season initialized yet - nothing to do
		return nil
	}

	// Check if we need to enter nomination phase
	if season.Status == types.SeasonStatus_SEASON_STATUS_ACTIVE {
		params, pErr := k.Params.Get(ctx)
		if pErr == nil {
			nominationWindowBlocks := commontypes.Saturate[int64](params.NominationWindowEpochs) * params.EpochBlocks
			nominationStartBlock := season.EndBlock - nominationWindowBlocks
			if currentBlock >= nominationStartBlock {
				season.Status = types.SeasonStatus_SEASON_STATUS_NOMINATION
				if err := k.Season.Set(ctx, season); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("BeginBlocker: Season.Set failed", "err", err)
				}
				sdkCtx.EventManager().EmitEvent(
					sdk.NewEvent(
						"nomination_window_opened",
						sdk.NewAttribute("season", fmt.Sprintf("%d", season.Number)),
						sdk.NewAttribute("start_block", fmt.Sprintf("%d", currentBlock)),
					),
				)
			}
		}
	}

	// Check if we need to start or continue a transition
	transitionState, transitionErr := k.SeasonTransitionState.Get(ctx)
	hasActiveTransition := transitionErr == nil &&
		transitionState.Phase != types.TransitionPhase_TRANSITION_PHASE_UNSPECIFIED

	if hasActiveTransition {
		// SEA-03: a transition in recovery mode stays halted until the
		// Commons Operations Committee or governance acts
		// (MsgRetrySeasonTransition / MsgSkipTransitionPhase /
		// MsgAbortSeasonTransition). Retrying a deterministic failure every
		// block would only burn work.
		if recovery, rErr := k.TransitionRecoveryState.Get(ctx); rErr == nil && recovery.RecoveryMode {
			return nil
		}
		k.checkTransitionWatchdog(ctx, transitionState)
		// Continue processing the active transition (including finalization at COMPLETE phase)
		k.processTransitionBatch(ctx, transitionState)
		return nil
	}

	// Check if season has ended and we need to start a transition
	if currentBlock >= season.EndBlock && (season.Status == types.SeasonStatus_SEASON_STATUS_ACTIVE || season.Status == types.SeasonStatus_SEASON_STATUS_NOMINATION) {
		// Never fail the block over a transition (SEA-03).
		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.startSeasonTransition(cacheCtx, season); err != nil {
			sdkCtx.Logger().Error("failed to start season transition", "season", season.Number, "error", err)
			return nil
		}
		write()
	}

	return nil
}

// checkTransitionWatchdog emits season_transition_overdue (once) when a
// transition has run longer than max_transition_epochs. It only signals: the
// transition keeps going, and recovery stays a human decision (SEA-19).
func (k Keeper) checkTransitionWatchdog(ctx context.Context, state types.SeasonTransitionState) {
	params, err := k.Params.Get(ctx)
	if err != nil || params.MaxTransitionEpochs == 0 || params.EpochBlocks <= 0 {
		return
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	limit := commontypes.Saturate[int64](params.MaxTransitionEpochs) * params.EpochBlocks
	if sdkCtx.BlockHeight()-state.TransitionStart != limit+1 {
		return
	}
	sdkCtx.Logger().Error("season transition overdue",
		"phase", state.Phase.String(), "started", state.TransitionStart, "limit_blocks", limit)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("season_transition_overdue",
		sdk.NewAttribute("phase", state.Phase.String()),
		sdk.NewAttribute("transition_start", fmt.Sprintf("%d", state.TransitionStart)),
		sdk.NewAttribute("max_transition_blocks", fmt.Sprintf("%d", limit)),
	))
}

// startSeasonTransition initializes a new season transition.
func (k Keeper) startSeasonTransition(ctx context.Context, season types.Season) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Update season status to ENDING
	season.Status = types.SeasonStatus_SEASON_STATUS_ENDING
	if err := k.Season.Set(ctx, season); err != nil {
		return err
	}

	// Initialize transition state
	transitionState := types.SeasonTransitionState{
		Phase:           types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS,
		ProcessedCount:  0,
		TotalCount:      0, // Will be set when we know how many members
		TransitionStart: sdkCtx.BlockHeight(),
		MaintenanceMode: false,
	}

	if err := k.SeasonTransitionState.Set(ctx, transitionState); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"season_transition_started",
			sdk.NewAttribute("from_season", fmt.Sprintf("%d", season.Number)),
			sdk.NewAttribute("to_season", fmt.Sprintf("%d", season.Number+1)),
			sdk.NewAttribute("start_block", fmt.Sprintf("%d", sdkCtx.BlockHeight())),
		),
	)

	return nil
}

// processTransitionBatch processes a batch of the current transition phase.
//
// SEA-03: the batch runs in a cache context and never fails the block. On
// error the half-applied batch is discarded, the failure is persisted in
// TransitionRecoveryState (entering recovery mode after
// transition_max_retries consecutive failures) and the block goes on.
func (k Keeper) processTransitionBatch(ctx context.Context, state types.SeasonTransitionState) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()
	if err := k.runTransitionBatch(cacheCtx, state); err != nil {
		sdkCtx.Logger().Error("season transition batch failed",
			"phase", state.Phase.String(), "error", err)
		k.handleTransitionError(ctx, state, err)
		return
	}
	write()

	// A successful batch ends a run of transient failures: forget the
	// count so a later failure gets the full retry budget again.
	if recovery, err := k.TransitionRecoveryState.Get(ctx); err == nil && !recovery.RecoveryMode {
		if err := k.TransitionRecoveryState.Remove(ctx); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("processTransitionBatch: TransitionRecoveryState.Remove failed", "err", err)
		}
	}
}

// runTransitionBatch does the work of one transition batch on ctx.
func (k Keeper) runTransitionBatch(ctx context.Context, state types.SeasonTransitionState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// TransitionBatchSize is validated > 0 by Params.Validate.
	batchSize := params.TransitionBatchSize

	var phaseComplete bool
	var processErr error

	switch state.Phase {
	case types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS:
		phaseComplete, processErr = k.processRetroRewardsPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES:
		phaseComplete, processErr = k.processReturnNominationStakesPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT:
		// Maintenance mode from SNAPSHOT on, so the reputation and DREAM
		// the snapshot records stop moving while it is taken (SEA-16).
		state.MaintenanceMode = true
		phaseComplete, processErr = k.processSnapshotPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION:
		// Enable maintenance mode for critical phases
		state.MaintenanceMode = true
		phaseComplete, processErr = k.processArchiveReputationPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION:
		state.MaintenanceMode = true
		phaseComplete, processErr = k.processResetReputationPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_RESET_XP:
		state.MaintenanceMode = true
		phaseComplete, processErr = k.processResetXPPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_TITLES:
		state.MaintenanceMode = false
		phaseComplete, processErr = k.processTitlesPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_CLEANUP:
		state.MaintenanceMode = false
		phaseComplete, processErr = k.processCleanupPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_COMPLETE:
		// Transition is complete - finalize
		return k.finalizeTransition(ctx, state)
	}

	if processErr != nil {
		// The caller discards this batch and records the failure.
		return processErr
	}

	if phaseComplete {
		// Move to next phase
		completedPhase := state.Phase
		state.Phase = k.nextTransitionPhase(state.Phase)
		state.ProcessedCount = 0
		state.LastProcessed = ""

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"season_transition_phase_complete",
				sdk.NewAttribute("completed_phase", completedPhase.String()),
				sdk.NewAttribute("next_phase", state.Phase.String()),
			),
		)
	}

	// Save updated state
	return k.SeasonTransitionState.Set(ctx, state)
}

// nextTransitionPhase returns the next phase in the transition sequence.
// Phases run: RETRO_REWARDS(8) -> RETURN_NOMINATION_STAKES(9) -> SNAPSHOT(1) -> ... -> COMPLETE(7)
func (k Keeper) nextTransitionPhase(current types.TransitionPhase) types.TransitionPhase {
	switch current {
	case types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS:
		return types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES
	case types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES:
		return types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT
	default:
		return current + 1
	}
}

// processSnapshotPhase creates member snapshots for the ending season.
func (k Keeper) processSnapshotPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get current season for snapshot
	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}

	// Create the overall season snapshot (once at start)
	if state.ProcessedCount == 0 && state.LastProcessed == "" {
		seasonSnapshot := types.SeasonSnapshot{
			Season:        season.Number,
			SnapshotBlock: sdkCtx.BlockHeight(),
		}
		if err := k.SeasonSnapshot.Set(ctx, season.Number, seasonSnapshot); err != nil {
			return false, err
		}
		// Seasonal titles are decided in this phase, batch by batch, while
		// season XP and levels are still intact (RESET_XP runs before
		// TITLES).
		if err := k.beginSeasonTitleEligibility(ctx, season.Number); err != nil {
			return false, err
		}
	}

	// Iterate through member profiles and create individual snapshots
	// Resume strictly after the cursor (SEA-05): no rescan from the first key.
	iter, err := k.MemberProfile.Iterate(ctx, cursorAfter(state.LastProcessed))
	if err != nil {
		return false, err
	}
	defer iter.Close()

	processed := 0
	var lastKey string
	var batch []types.MemberProfile
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return false, err // a decode error must not skip a member (SEA-22)
		}
		lastKey = kv.Key
		batch = append(batch, kv.Value)
		processed++
	}
	done := !iter.Valid()

	eligibleIDs, err := k.accumulateSeasonTitleEligibility(ctx, season.Number, batch)
	if err != nil {
		return false, err
	}

	for _, profile := range batch {
		// Create member season snapshot
		snapshotKey := fmt.Sprintf("%d/%s", season.Number, profile.Address)

		// DREAM balance, reputation and completed initiatives from x/rep.
		// A season profile with no rep member records zeros; any other
		// read error fails the batch so recovery engages instead of
		// archiving a silent zero (SEA-22).
		dreamBalance := math.ZeroInt()
		reputation := make(map[string]string)
		var initiativesCompleted uint64
		if k.late.repKeeper != nil {
			// A profile key that is not an address has no DREAM balance.
			if addrBytes, err := k.addressCodec.StringToBytes(profile.Address); err == nil {
				balance, err := k.late.repKeeper.GetBalance(ctx, addrBytes)
				if err != nil && !errors.Is(err, reptypes.ErrMemberNotFound) {
					return false, err
				}
				if err == nil {
					dreamBalance = balance
				}
			}
			scores, err := k.late.repKeeper.GetReputationScores(ctx, profile.Address)
			if err != nil && !errors.Is(err, reptypes.ErrMemberNotFound) {
				return false, err
			}
			if err == nil && scores != nil {
				reputation = scores
			}
			count, err := k.late.repKeeper.GetCompletedInitiativesCount(ctx, profile.Address)
			if err != nil && !errors.Is(err, reptypes.ErrMemberNotFound) {
				return false, err
			}
			if err == nil {
				initiativesCompleted = count
			}
		}

		snapshot := types.MemberSeasonSnapshot{
			SeasonAddress:        snapshotKey,
			FinalDreamBalance:    dreamBalance,
			FinalReputation:      reputation,
			InitiativesCompleted: initiativesCompleted,
			XpEarned:             profile.SeasonXp,
			SeasonLevel:          profile.SeasonLevel,
			AchievementsEarned:   profile.Achievements,
			EligibleTitleIds:     eligibleIDs[profile.Address],
		}

		if err := k.MemberSeasonSnapshot.Set(ctx, snapshotKey, snapshot); err != nil {
			return false, err
		}
	}

	// Update state
	state.ProcessedCount += commontypes.Saturate[uint64](processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	// Check if we've processed all members
	if done {
		if err := k.finishSeasonTitleEligibility(ctx, season.Number); err != nil {
			return false, err
		}
	}
	return done, nil
}

// cursorAfter returns the range that resumes a string-keyed batch walk
// strictly after the last processed key, or nil (walk everything) when the
// phase has not started (SEA-05).
func cursorAfter(last string) collections.Ranger[string] {
	if last == "" {
		return nil
	}
	return new(collections.Range[string]).StartExclusive(last)
}

// processArchiveReputationPhase archives reputation scores.
// This phase iterates through members and archives their reputation to the MemberSeasonSnapshot.
// Note: Full implementation requires x/rep keeper methods for getting reputation per tag.
func (k Keeper) processArchiveReputationPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	// Get current season for key generation
	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}

	// Iterate through member profiles
	// Resume strictly after the cursor (SEA-05): no rescan from the first key.
	iter, err := k.MemberProfile.Iterate(ctx, cursorAfter(state.LastProcessed))
	if err != nil {
		return false, err
	}
	defer iter.Close()

	processed := 0
	var lastKey string
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return false, err // a decode error must not skip a member (SEA-22)
		}

		profile := kv.Value
		lastKey = kv.Key

		// Update the member's season snapshot with reputation data
		snapshotKey := fmt.Sprintf("%d/%s", season.Number, profile.Address)
		snapshot, err := k.MemberSeasonSnapshot.Get(ctx, snapshotKey)
		if err != nil {
			// Snapshot doesn't exist, skip
			processed++
			continue
		}

		// Get reputation scores from x/rep and update snapshot
		if k.late.repKeeper != nil {
			scores, err := k.late.repKeeper.GetReputationScores(ctx, profile.Address)
			if err != nil && !errors.Is(err, reptypes.ErrMemberNotFound) {
				return false, err
			}
			if err == nil {
				snapshot.FinalReputation = scores
			}
		}

		if err := k.MemberSeasonSnapshot.Set(ctx, snapshotKey, snapshot); err != nil {
			return false, err
		}

		processed++
	}

	// Update state
	state.ProcessedCount += commontypes.Saturate[uint64](processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	return !iter.Valid(), nil
}

// processResetReputationPhase resets reputation scores for the new season.
//
// It walks x/rep's member records, not season profiles (SEA-06): a member who
// earned reputation but never touched x/season has no profile, and walking
// profiles left that member's seasonal reputation unreset into the next
// season. The cursor is the last member address processed.
func (k Keeper) processResetReputationPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	if k.late.repKeeper != nil {
		members, done, err := k.late.repKeeper.MembersAfter(ctx, state.LastProcessed, batchSize)
		if err != nil {
			return false, err
		}
		for _, addr := range members {
			if _, err := k.late.repKeeper.ArchiveSeasonalReputation(ctx, addr); err != nil {
				// SEA-22: fail the batch rather than advance the cursor
				// past an unreset member. The batch is discarded, retried,
				// and after transition_max_retries enters recovery mode,
				// where the committee retries or skips the phase.
				return false, fmt.Errorf("archive seasonal reputation of %s: %w", addr, err)
			}
		}
		state.ProcessedCount += uint64(len(members))
		if len(members) > 0 {
			state.LastProcessed = members[len(members)-1]
		}
		return done, nil
	}

	// No x/rep wired: nothing to reset. Walk the profiles so the phase
	// still advances the same way.
	// Iterate through member profiles
	// Resume strictly after the cursor (SEA-05): no rescan from the first key.
	iter, err := k.MemberProfile.Iterate(ctx, cursorAfter(state.LastProcessed))
	if err != nil {
		return false, err
	}
	defer iter.Close()

	processed := 0
	var lastKey string
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return false, err // a decode error must not skip a member (SEA-22)
		}

		profile := kv.Value
		lastKey = kv.Key

		// Archive seasonal reputation to lifetime and reset for new season
		// This is the critical step that:
		// 1. Adds seasonal scores to lifetime totals
		// 2. Clears seasonal reputation scores
		if k.late.repKeeper != nil {
			if _, err := k.late.repKeeper.ArchiveSeasonalReputation(ctx, profile.Address); err != nil {
				return false, fmt.Errorf("archive seasonal reputation of %s: %w", profile.Address, err) // SEA-22
			}
		}

		processed++
	}

	// Update state
	state.ProcessedCount += commontypes.Saturate[uint64](processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	return !iter.Valid(), nil
}

// processResetXPPhase resets XP for the new season.
func (k Keeper) processResetXPPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	// Get current season number for snapshot
	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}

	// Iterate through member profiles and reset season XP
	// Resume strictly after the cursor (SEA-05): no rescan from the first key.
	iter, err := k.MemberProfile.Iterate(ctx, cursorAfter(state.LastProcessed))
	if err != nil {
		return false, err
	}
	defer iter.Close()

	processed := 0
	var lastKey string
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return false, err // a decode error must not skip a member (SEA-22)
		}

		profile := kv.Value
		lastKey = kv.Key

		// Record the pre-reset XP/level in the season history entry. The
		// SNAPSHOT phase already wrote the full record (DREAM balance,
		// reputation, initiatives, achievements), so update only the XP
		// fields in place; a fresh minimal record is written only when no
		// snapshot exists (e.g. the SNAPSHOT phase was skipped).
		if profile.SeasonXp > 0 {
			snapshotKey := fmt.Sprintf("%d/%s", season.Number, profile.Address)
			snapshot, err := k.MemberSeasonSnapshot.Get(ctx, snapshotKey)
			if err != nil {
				snapshot = types.MemberSeasonSnapshot{SeasonAddress: snapshotKey}
			}
			snapshot.XpEarned = profile.SeasonXp
			snapshot.SeasonLevel = profile.SeasonLevel
			if err := k.MemberSeasonSnapshot.Set(ctx, snapshotKey, snapshot); err != nil {
				return false, err
			}
		}

		// Reset season XP (keep lifetime XP)
		profile.SeasonXp = 0
		profile.SeasonLevel = 1
		if err := k.MemberProfile.Set(ctx, kv.Key, profile); err != nil {
			return false, err
		}
		processed++
	}

	// Update state
	state.ProcessedCount += commontypes.Saturate[uint64](processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	return !iter.Valid(), nil
}

// processTitlesPhase grants the season's titles. Eligibility was computed
// in the SNAPSHOT phase; each eligible member receives the per-season
// `s<N>_<title>` title, which is permanent (UnlockTitle rolls the oldest
// titles into the archive past the display cap).
func (k Keeper) processTitlesPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	// Get current season to check season-specific titles
	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}
	eligibility, err := k.SeasonTitleEligibility.Get(ctx, season.Number)
	if err != nil {
		eligibility = types.SeasonTitleEligibility{TitleSeason: season.Number, Season: season.Number}
	}

	// Iterate through member profiles
	// Resume strictly after the cursor (SEA-05): no rescan from the first key.
	iter, err := k.MemberProfile.Iterate(ctx, cursorAfter(state.LastProcessed))
	if err != nil {
		return false, err
	}
	defer iter.Close()

	processed := 0
	var lastKey string
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return false, err // a decode error must not skip a member (SEA-22)
		}

		lastKey = kv.Key
		var thresholdIDs []string
		if snap, err := k.MemberSeasonSnapshot.Get(ctx, fmt.Sprintf("%d/%s", season.Number, kv.Value.Address)); err == nil {
			thresholdIDs = snap.EligibleTitleIds
		}
		if err := k.grantSeasonalTitles(ctx, season.Number, eligibility, kv.Key, thresholdIDs); err != nil {
			return false, fmt.Errorf("grant seasonal titles to %s: %w", kv.Key, err) // SEA-22
		}

		processed++
	}

	// Update state
	state.ProcessedCount += commontypes.Saturate[uint64](processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	return !iter.Valid(), nil
}

// processCleanupPhase expires the ending season's quests (SEA-26).
//
// Completed progress is history and is always kept: non-repeatable quests
// read it to refuse a second claim, repeatable ones for the cooldown, and
// later quests for their prerequisite. Open (unclaimed) progress is dropped
// when its quest is gone, deactivated, past its end block or tied to the
// ending (or an earlier) season, so a seasonal quest cannot be claimed in a
// later season. The first batch also deactivates the ending season's
// seasonal quests (the quest table is committee-curated and small).
func (k Keeper) processCleanupPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}
	if state.LastProcessed == "" {
		if err := k.deactivateSeasonQuests(ctx, season.Number); err != nil {
			return false, err
		}
	}

	// Resume strictly after the cursor (SEA-05): no rescan from the first key.
	iter, err := k.MemberQuestProgress.Iterate(ctx, cursorAfter(state.LastProcessed))
	if err != nil {
		return false, err
	}
	defer iter.Close()

	currentBlock := sdk.UnwrapSDKContext(ctx).BlockHeight()
	processed := 0
	var lastKey string
	var keysToDelete []string

	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			return false, err
		}
		lastKey = kv.Key
		processed++

		if kv.Value.Completed {
			continue // history: never deleted
		}
		quest, err := k.Quest.Get(ctx, extractQuestIdFromKey(kv.Key))
		switch {
		case err != nil,
			!quest.Active,
			quest.EndBlock > 0 && currentBlock > quest.EndBlock,
			quest.Season > 0 && quest.Season <= season.Number:
			keysToDelete = append(keysToDelete, kv.Key)
		}
	}
	done := !iter.Valid()

	for _, key := range keysToDelete {
		if err := k.MemberQuestProgress.Remove(ctx, key); err != nil {
			return false, err
		}
	}

	// Update state
	state.ProcessedCount += commontypes.Saturate[uint64](processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	return done, nil
}

// deactivateSeasonQuests marks every active quest bound to seasonNumber (or
// an earlier season) inactive at the end of that season (SEA-26).
func (k Keeper) deactivateSeasonQuests(ctx context.Context, seasonNumber uint64) error {
	var ended []types.Quest
	if err := k.Quest.Walk(ctx, nil, func(_ string, q types.Quest) (bool, error) {
		if q.Active && q.Season > 0 && q.Season <= seasonNumber {
			ended = append(ended, q)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, q := range ended {
		q.Active = false
		if err := k.Quest.Set(ctx, q.QuestId, q); err != nil {
			return err
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("quest_expired",
			sdk.NewAttribute("quest_id", q.QuestId),
			sdk.NewAttribute("season", fmt.Sprintf("%d", q.Season)),
		))
	}
	return nil
}

// extractQuestIdFromKey extracts the quest ID from a composite
// MemberQuestProgress key, "member_address:quest_id" (see questProgressKey).
func extractQuestIdFromKey(key string) string {
	if i := strings.Index(key, ":"); i >= 0 {
		return key[i+1:]
	}
	return ""
}

// finalizeTransition completes the season transition.
func (k Keeper) finalizeTransition(ctx context.Context, state types.SeasonTransitionState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get current season to determine next season number
	oldSeason, err := k.Season.Get(ctx)
	if err != nil {
		return err
	}

	// Get next season info
	nextSeasonInfo, getErr := k.NextSeasonInfo.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(ctx).Logger().Error("finalizeTransition: NextSeasonInfo.Get failed", "err", getErr)
	}

	// Get params for season duration
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(ctx).Logger().Error("finalizeTransition: Params.Get failed", "err", getErr)
	}
	epochBlocks := params.EpochBlocks
	if epochBlocks == 0 {
		epochBlocks = 14400 // Default; keep in lockstep with x/rep's epoch length
	}
	seasonDurationEpochs := params.SeasonDurationEpochs
	if seasonDurationEpochs == 0 {
		seasonDurationEpochs = 100 // Default
	}

	// Create new season
	newSeasonNumber := oldSeason.Number + 1
	newSeason := types.Season{
		Number:     newSeasonNumber,
		Name:       nextSeasonInfo.Name,
		Theme:      nextSeasonInfo.Theme,
		StartBlock: sdkCtx.BlockHeight(),
		StartTime:  sdkCtx.BlockTime().Unix(),
		EndBlock:   sdkCtx.BlockHeight() + int64(epochBlocks*seasonDurationEpochs),
		Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
	}

	// Set default name if not provided
	if newSeason.Name == "" {
		newSeason.Name = fmt.Sprintf("Season %d", newSeason.Number)
	}

	if err := k.Season.Set(ctx, newSeason); err != nil {
		return err
	}
	// Archive the finished season's running aggregates (SEA-35).
	if err := k.archiveSeasonStats(ctx, oldSeason.Number, newSeasonNumber); err != nil {
		return err
	}

	// Refill x/rep's seasonal staking reward budget and reset its per-season
	// economic counters for the incoming season. Non-fatal: a failure here must
	// not strand the chain mid-transition with the old season already cleared.
	// SEA-22: it runs in its own cache context (no partial counter reset)
	// and a failure is remembered in PendingSeasonalPoolInit, which the
	// BeginBlocker retries.
	if k.late.repKeeper != nil {
		if err := k.initSeasonalPool(ctx, newSeasonNumber); err != nil {
			sdkCtx.Logger().Error("failed to initialise rep seasonal staking pool",
				"season", newSeasonNumber, "error", err)
			if err := k.PendingSeasonalPoolInit.Set(ctx, newSeasonNumber); err != nil {
				return err
			}
		}
	}

	// Clear transition state
	if err := k.SeasonTransitionState.Remove(ctx); err != nil {
		return err
	}

	// Clear next season info
	if err := k.NextSeasonInfo.Remove(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("finalizeTransition: NextSeasonInfo.Remove failed", "err", err)
	}

	// Clear recovery state if any
	if err := k.TransitionRecoveryState.Remove(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("finalizeTransition: TransitionRecoveryState.Remove failed", "err", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"season_transition_complete",
			sdk.NewAttribute("new_season_number", fmt.Sprintf("%d", newSeason.Number)),
			sdk.NewAttribute("new_season_name", newSeason.Name),
			sdk.NewAttribute("start_block", fmt.Sprintf("%d", newSeason.StartBlock)),
			sdk.NewAttribute("end_block", fmt.Sprintf("%d", newSeason.EndBlock)),
		),
	)

	return nil
}

// initSeasonalPool runs x/rep's InitSeasonalPool in a cache context and
// commits it only on success.
func (k Keeper) initSeasonalPool(ctx context.Context, season uint64) error {
	cacheCtx, write := sdk.UnwrapSDKContext(ctx).CacheContext()
	if err := k.late.repKeeper.InitSeasonalPool(cacheCtx, season); err != nil {
		return err
	}
	write()
	return nil
}

// retryPendingSeasonalPoolInit retries a failed InitSeasonalPool once per
// block until it succeeds or a later season supersedes it (SEA-22).
func (k Keeper) retryPendingSeasonalPoolInit(ctx context.Context) {
	pending, err := k.PendingSeasonalPoolInit.Get(ctx)
	if err != nil || k.late.repKeeper == nil {
		return
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if season, err := k.Season.Get(ctx); err == nil && season.Number != pending {
		// A later transition already initialised its own season.
		if err := k.PendingSeasonalPoolInit.Remove(ctx); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("retryPendingSeasonalPoolInit: PendingSeasonalPoolInit.Remove failed", "err", err)
		}
		return
	}
	if err := k.initSeasonalPool(ctx, pending); err != nil {
		sdkCtx.Logger().Error("retry of rep seasonal pool init failed", "season", pending, "error", err)
		return
	}
	if err := k.PendingSeasonalPoolInit.Remove(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("retryPendingSeasonalPoolInit: PendingSeasonalPoolInit.Remove failed", "err", err)
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("seasonal_pool_initialised",
		sdk.NewAttribute("season", fmt.Sprintf("%d", pending)),
		sdk.NewAttribute("retried", "true"),
	))
}

// handleTransitionError handles errors during transition processing.
func (k Keeper) handleTransitionError(ctx context.Context, state types.SeasonTransitionState, err error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get or create recovery state
	recovery, recoveryErr := k.TransitionRecoveryState.Get(ctx)
	if recoveryErr != nil {
		recovery = types.TransitionRecoveryState{
			FailedPhase:  state.Phase,
			FailureCount: 0,
		}
	}

	recovery.FailureCount++
	recovery.LastError = err.Error()
	recovery.LastAttemptBlock = sdkCtx.BlockHeight()

	// Get params for max retries
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(ctx).Logger().Error("handleTransitionError: Params.Get failed", "err", getErr)
	}
	// TransitionMaxRetries is validated > 0 by Params.Validate.
	maxRetries := params.TransitionMaxRetries

	if recovery.FailureCount >= uint64(maxRetries) {
		recovery.RecoveryMode = true
	}

	if err := k.TransitionRecoveryState.Set(ctx, recovery); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("handleTransitionError: TransitionRecoveryState.Set failed", "err", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"season_transition_error",
			sdk.NewAttribute("phase", state.Phase.String()),
			sdk.NewAttribute("error", err.Error()),
			sdk.NewAttribute("failure_count", fmt.Sprintf("%d", recovery.FailureCount)),
			sdk.NewAttribute("recovery_mode", fmt.Sprintf("%t", recovery.RecoveryMode)),
		),
	)
}

// processExpiredModerations auto-resolves display name moderations where the appeal period
// has expired without an appeal. For each expired moderation:
// - The report is upheld (name stays cleared)
// - The reporter's DREAM stake is unlocked (returned)
// - The moderation record is closed
//
// SEA-09: walks ModerationQueue, which holds only active unappealed records
// ordered by moderated_at, so the oldest expired records are always resolved
// first and resolved or appealed records never consume the per-block budget.
// Each resolution runs in its own cache context. A failure keeps the record
// queued: it moves to ModerationRetryQueue at a later height with doubling
// backoff, and after MaxModerationAutoResolveAttempts it leaves the queues
// (record still active, stake still locked) for
// MsgResolveUnappealedModeration. Due retries share the per-block budget.
func (k Keeper) processExpiredModerations(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return
	}

	// Batch limit: process at most 50 moderations per block (first
	// attempts and retries together) to bound gas usage.
	const maxPerBlock = 50
	processed := 0

	// Expired when currentBlock > moderated_at + appeal_period, i.e.
	// moderated_at <= currentBlock - appeal_period - 1.
	cutoff := currentBlock - commontypes.Saturate[int64](params.DisplayNameAppealPeriodBlocks) - 1
	if cutoff >= 0 {
		var due []collections.Pair[int64, string]
		if err := k.ModerationQueue.Walk(ctx, collections.NewPrefixUntilPairRange[int64, string](cutoff),
			func(key collections.Pair[int64, string]) (bool, error) {
				due = append(due, key)
				return len(due) >= maxPerBlock, nil
			}); err != nil {
			sdkCtx.Logger().Error("failed to walk moderation queue", "error", err)
			return
		}
		for _, key := range due {
			processed++
			member := key.K2()
			moderation, err := k.DisplayNameModeration.Get(ctx, member)
			if err != nil || !moderationQueued(moderation) || moderation.ModeratedAt != key.K1() {
				// Stale index entry: drop it.
				if err := k.ModerationQueue.Remove(ctx, key); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("processExpiredModerations: ModerationQueue.Remove failed", "err", err)
				}
				continue
			}
			k.autoResolveModeration(ctx, sdkCtx, member, moderation)
		}
	}

	if processed >= maxPerBlock {
		return
	}
	var retries []collections.Pair[int64, string]
	if err := k.ModerationRetryQueue.Walk(ctx, collections.NewPrefixUntilPairRange[int64, string](currentBlock),
		func(key collections.Pair[int64, string]) (bool, error) {
			retries = append(retries, key)
			return processed+len(retries) >= maxPerBlock, nil
		}); err != nil {
		sdkCtx.Logger().Error("failed to walk moderation retry queue", "error", err)
		return
	}
	for _, key := range retries {
		member := key.K2()
		moderation, err := k.DisplayNameModeration.Get(ctx, member)
		if err != nil || !moderationRetryQueued(moderation) || moderation.AutoResolveRetryAt != key.K1() {
			if err := k.ModerationRetryQueue.Remove(ctx, key); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("processExpiredModerations: ModerationRetryQueue.Remove failed", "err", err)
			}
			continue
		}
		k.autoResolveModeration(ctx, sdkCtx, member, moderation)
	}
}

// autoResolveModeration upholds an expired, unappealed report in a cache
// context. On failure nothing of the attempt commits; the record is
// re-queued with backoff, or, after the last attempt, left active and out
// of the queues for MsgResolveUnappealedModeration (SEA-09).
func (k Keeper) autoResolveModeration(ctx context.Context, sdkCtx sdk.Context, member string, moderation types.DisplayNameModeration) {
	cacheCtx, write := sdkCtx.CacheContext()
	err := k.ResolveUnappealedModerationInternal(cacheCtx, member)
	if err == nil {
		write()
		return
	}
	moderation.AutoResolveAttempts++
	attempt := moderation.AutoResolveAttempts
	if attempt >= MaxModerationAutoResolveAttempts {
		moderation.AutoResolveRetryAt = 0
	} else {
		moderation.AutoResolveRetryAt = sdkCtx.BlockHeight() + int64(ModerationRetryBaseBlocks)<<(attempt-1)
	}
	sdkCtx.Logger().Error("failed to auto-resolve expired moderation",
		"member", member, "attempt", attempt, "error", err)
	if serr := k.SetDisplayNameModeration(ctx, member, moderation); serr != nil {
		sdkCtx.Logger().Error("failed to re-queue moderation auto-resolution",
			"member", member, "error", serr)
		return
	}
	evType := "display_name_moderation_auto_resolve_deferred"
	if moderation.AutoResolveRetryAt == 0 {
		evType = "display_name_moderation_auto_resolve_abandoned"
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(evType,
		sdk.NewAttribute("member", member),
		sdk.NewAttribute("attempt", fmt.Sprintf("%d", attempt)),
		sdk.NewAttribute("retry_at", fmt.Sprintf("%d", moderation.AutoResolveRetryAt)),
		sdk.NewAttribute("error", err.Error()),
	))
}

// processExpiredAppeals times out appeals that nobody resolved within
// AppealResolutionPeriodMultiplier x display_name_appeal_period_blocks of
// the appeal (SEA-23): both stakes are
// returned and the name restored, so an idle resolver can never lock two
// stakes and the member's display name for good. Walks AppealQueue, which
// holds only active appealed records ordered by appealed_at.
func (k Keeper) processExpiredAppeals(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return
	}
	const maxPerBlock = 50
	// Due when appealed_at + window < now, i.e. appealed_at <= now - window - 1.
	cutoff := sdkCtx.BlockHeight() - (appealDeadline(0, params)) - 1
	if cutoff < 0 {
		return
	}
	var due []collections.Pair[int64, string]
	if err := k.AppealQueue.Walk(ctx, collections.NewPrefixUntilPairRange[int64, string](cutoff),
		func(key collections.Pair[int64, string]) (bool, error) {
			due = append(due, key)
			return len(due) >= maxPerBlock, nil
		}); err != nil {
		sdkCtx.Logger().Error("failed to walk appeal queue", "error", err)
		return
	}
	for _, key := range due {
		member := key.K2()
		moderation, err := k.DisplayNameModeration.Get(ctx, member)
		if err != nil || !appealQueued(moderation) || moderation.AppealedAt != key.K1() {
			if err := k.AppealQueue.Remove(ctx, key); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("processExpiredAppeals: AppealQueue.Remove failed", "err", err)
			}
			continue
		}
		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.ResolveAppealTimeoutInternal(cacheCtx, member); err != nil {
			// Drop the entry so one failing record cannot starve the rest;
			// MsgResolveDisplayNameAppeal still resolves it.
			sdkCtx.Logger().Error("failed to time out display name appeal", "member", member, "error", err)
			if err := k.AppealQueue.Remove(ctx, key); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("processExpiredAppeals: AppealQueue.Remove failed", "err", err)
			}
			continue
		}
		write()
	}
}
