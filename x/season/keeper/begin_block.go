package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

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
			nominationWindowBlocks := int64(params.NominationWindowEpochs) * params.EpochBlocks
			nominationStartBlock := season.EndBlock - nominationWindowBlocks
			if currentBlock >= nominationStartBlock {
				season.Status = types.SeasonStatus_SEASON_STATUS_NOMINATION
				_ = k.Season.Set(ctx, season)
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
		// Continue processing the active transition (including finalization at COMPLETE phase)
		return k.processTransitionBatch(ctx, transitionState)
	}

	// Check if season has ended and we need to start a transition
	if currentBlock >= season.EndBlock && (season.Status == types.SeasonStatus_SEASON_STATUS_ACTIVE || season.Status == types.SeasonStatus_SEASON_STATUS_NOMINATION) {
		return k.startSeasonTransition(ctx, season)
	}

	return nil
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
func (k Keeper) processTransitionBatch(ctx context.Context, state types.SeasonTransitionState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	batchSize := params.TransitionBatchSize
	if batchSize == 0 {
		batchSize = 100 // Default batch size
	}

	var phaseComplete bool
	var processErr error

	switch state.Phase {
	case types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS:
		phaseComplete, processErr = k.processRetroRewardsPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES:
		phaseComplete, processErr = k.processReturnNominationStakesPhase(ctx, &state, int(batchSize))

	case types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT:
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
		// Handle error - enter recovery mode
		k.handleTransitionError(ctx, state, processErr)
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
	if state.ProcessedCount == 0 {
		seasonSnapshot := types.SeasonSnapshot{
			Season:        season.Number,
			SnapshotBlock: sdkCtx.BlockHeight(),
		}
		if err := k.SeasonSnapshot.Set(ctx, season.Number, seasonSnapshot); err != nil {
			return false, err
		}
	}

	// Iterate through member profiles and create individual snapshots
	var startKey *string
	if state.LastProcessed != "" {
		startKey = &state.LastProcessed
	}

	iter, err := k.MemberProfile.Iterate(ctx, nil)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	// Skip to last processed if we're resuming
	if startKey != nil {
		for iter.Valid() {
			key, err := iter.Key()
			if err != nil {
				break
			}
			if key > *startKey {
				break
			}
			iter.Next()
		}
	}

	processed := 0
	var lastKey string
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}

		profile := kv.Value
		lastKey = kv.Key

		// Create member season snapshot
		snapshotKey := fmt.Sprintf("%d/%s", season.Number, profile.Address)

		// Get DREAM balance from rep keeper if available
		var dreamBalance int64
		if k.repKeeper != nil {
			addrBytes, err := k.addressCodec.StringToBytes(profile.Address)
			if err == nil {
				balance, err := k.repKeeper.GetBalance(ctx, addrBytes)
				if err == nil {
					dreamBalance = balance.Int64()
				}
			}
		}

		// Get reputation scores from x/rep
		reputation := make(map[string]string)
		if k.repKeeper != nil {
			if scores, err := k.repKeeper.GetReputationScores(ctx, profile.Address); err == nil {
				reputation = scores
			}
		}

		// Get completed initiatives count from x/rep
		var initiativesCompleted uint64
		if k.repKeeper != nil {
			if count, err := k.repKeeper.GetCompletedInitiativesCount(ctx, profile.Address); err == nil {
				initiativesCompleted = count
			}
		}

		snapshot := types.MemberSeasonSnapshot{
			SeasonAddress:        snapshotKey,
			FinalDreamBalance:    math.NewInt(dreamBalance),
			FinalReputation:      reputation,
			InitiativesCompleted: initiativesCompleted,
			XpEarned:             profile.SeasonXp,
			SeasonLevel:          profile.SeasonLevel,
			AchievementsEarned:   profile.Achievements,
		}

		if err := k.MemberSeasonSnapshot.Set(ctx, snapshotKey, snapshot); err != nil {
			continue // Skip on error, don't fail the whole batch
		}

		processed++
	}

	// Update state
	state.ProcessedCount += uint64(processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	// Check if we've processed all members
	return !iter.Valid(), nil
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
	var startKey *string
	if state.LastProcessed != "" {
		startKey = &state.LastProcessed
	}

	iter, err := k.MemberProfile.Iterate(ctx, nil)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	// Skip to last processed if we're resuming
	if startKey != nil {
		for iter.Valid() {
			key, err := iter.Key()
			if err != nil {
				break
			}
			if key > *startKey {
				break
			}
			iter.Next()
		}
	}

	processed := 0
	var lastKey string
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
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
		if k.repKeeper != nil {
			if scores, err := k.repKeeper.GetReputationScores(ctx, profile.Address); err == nil {
				snapshot.FinalReputation = scores
			}
		}

		if err := k.MemberSeasonSnapshot.Set(ctx, snapshotKey, snapshot); err != nil {
			continue
		}

		processed++
	}

	// Update state
	state.ProcessedCount += uint64(processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	return !iter.Valid(), nil
}

// processResetReputationPhase resets reputation scores for the new season.
// This phase iterates through members and resets their seasonal reputation.
// Note: Full implementation requires x/rep keeper methods for resetting reputation.
func (k Keeper) processResetReputationPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	// Iterate through member profiles
	var startKey *string
	if state.LastProcessed != "" {
		startKey = &state.LastProcessed
	}

	iter, err := k.MemberProfile.Iterate(ctx, nil)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	// Skip to last processed if we're resuming
	if startKey != nil {
		for iter.Valid() {
			key, err := iter.Key()
			if err != nil {
				break
			}
			if key > *startKey {
				break
			}
			iter.Next()
		}
	}

	processed := 0
	var lastKey string
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}

		profile := kv.Value
		lastKey = kv.Key

		// Archive seasonal reputation to lifetime and reset for new season
		// This is the critical step that:
		// 1. Adds seasonal scores to lifetime totals
		// 2. Clears seasonal reputation scores
		if k.repKeeper != nil {
			_, _ = k.repKeeper.ArchiveSeasonalReputation(ctx, profile.Address)
		}

		processed++
	}

	// Update state
	state.ProcessedCount += uint64(processed)
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
	var startKey *string
	if state.LastProcessed != "" {
		startKey = &state.LastProcessed
	}

	iter, err := k.MemberProfile.Iterate(ctx, nil)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	// Skip to last processed if we're resuming
	if startKey != nil {
		for iter.Valid() {
			key, err := iter.Key()
			if err != nil {
				break
			}
			if key > *startKey {
				break
			}
			iter.Next()
		}
	}

	processed := 0
	var lastKey string
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}

		profile := kv.Value
		lastKey = kv.Key

		// Create season history entry before reset
		if profile.SeasonXp > 0 {
			snapshotKey := fmt.Sprintf("%d/%s", season.Number, profile.Address)
			snapshot := types.MemberSeasonSnapshot{
				SeasonAddress: snapshotKey,
				XpEarned:      profile.SeasonXp,
				SeasonLevel:   profile.SeasonLevel,
			}
			_ = k.MemberSeasonSnapshot.Set(ctx, snapshotKey, snapshot)
		}

		// Reset season XP (keep lifetime XP)
		profile.SeasonXp = 0
		profile.SeasonLevel = 1
		_ = k.MemberProfile.Set(ctx, kv.Key, profile)
		processed++
	}

	// Update state
	state.ProcessedCount += uint64(processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	return !iter.Valid(), nil
}

// processTitlesPhase handles title transitions.
// This phase moves seasonal titles from UnlockedTitles to ArchivedTitles.
func (k Keeper) processTitlesPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	// Get current season to check season-specific titles
	season, err := k.Season.Get(ctx)
	if err != nil {
		return false, err
	}

	// Iterate through member profiles
	var startKey *string
	if state.LastProcessed != "" {
		startKey = &state.LastProcessed
	}

	iter, err := k.MemberProfile.Iterate(ctx, nil)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	// Skip to last processed if we're resuming
	if startKey != nil {
		for iter.Valid() {
			key, err := iter.Key()
			if err != nil {
				break
			}
			if key > *startKey {
				break
			}
			iter.Next()
		}
	}

	processed := 0
	var lastKey string
	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}

		profile := kv.Value
		lastKey = kv.Key
		modified := false

		// Check each unlocked title and archive seasonal ones
		var remainingTitles []string
		for _, titleId := range profile.UnlockedTitles {
			title, err := k.Title.Get(ctx, titleId)
			if err != nil {
				// Title doesn't exist, keep it anyway
				remainingTitles = append(remainingTitles, titleId)
				continue
			}

			// Check if title is seasonal and should be archived
			if title.Seasonal {
				// Archive the seasonal title with season info
				archivedTitle := fmt.Sprintf("%d:%s", season.Number, titleId)
				profile.ArchivedTitles = append(profile.ArchivedTitles, archivedTitle)
				modified = true

				// If this was the display title, clear it
				if profile.DisplayTitle == titleId {
					profile.DisplayTitle = ""
				}
			} else {
				// Keep non-seasonal titles
				remainingTitles = append(remainingTitles, titleId)
			}
		}

		if modified {
			profile.UnlockedTitles = remainingTitles
			if err := k.MemberProfile.Set(ctx, kv.Key, profile); err != nil {
				continue
			}
		}

		processed++
	}

	// Update state
	state.ProcessedCount += uint64(processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	return !iter.Valid(), nil
}

// processCleanupPhase cleans up temporary data.
// This phase removes completed quest progress for non-repeatable quests
// and cleans up other season-specific temporary data.
func (k Keeper) processCleanupPhase(ctx context.Context, state *types.SeasonTransitionState, batchSize int) (bool, error) {
	// Iterate through quest progress
	var startKey *string
	if state.LastProcessed != "" {
		startKey = &state.LastProcessed
	}

	iter, err := k.MemberQuestProgress.Iterate(ctx, nil)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	// Skip to last processed if we're resuming
	if startKey != nil {
		for iter.Valid() {
			key, err := iter.Key()
			if err != nil {
				break
			}
			if key > *startKey {
				break
			}
			iter.Next()
		}
	}

	processed := 0
	var lastKey string
	var keysToDelete []string

	for ; iter.Valid() && processed < batchSize; iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}

		progress := kv.Value
		lastKey = kv.Key

		// Only process completed quests
		if !progress.Completed {
			processed++
			continue
		}

		// Extract quest ID from the composite key (member/quest_id format)
		// The key format is "member_address/quest_id"
		questId := extractQuestIdFromKey(kv.Key)
		if questId == "" {
			processed++
			continue
		}

		// Get the quest to check if it's repeatable
		quest, err := k.Quest.Get(ctx, questId)
		if err != nil {
			// Quest doesn't exist, safe to delete the progress
			keysToDelete = append(keysToDelete, kv.Key)
			processed++
			continue
		}

		// Delete progress for non-repeatable completed quests
		// For repeatable quests, we keep the progress for cooldown tracking
		if !quest.Repeatable {
			keysToDelete = append(keysToDelete, kv.Key)
		}

		processed++
	}

	// Delete collected keys
	for _, key := range keysToDelete {
		_ = k.MemberQuestProgress.Remove(ctx, key)
	}

	// Update state
	state.ProcessedCount += uint64(processed)
	if lastKey != "" {
		state.LastProcessed = lastKey
	}

	return !iter.Valid(), nil
}

// extractQuestIdFromKey extracts the quest ID from a composite key.
// Key format: "member_address/quest_id"
func extractQuestIdFromKey(key string) string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			return key[i+1:]
		}
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
	nextSeasonInfo, _ := k.NextSeasonInfo.Get(ctx)

	// Get params for season duration
	params, _ := k.Params.Get(ctx)
	epochBlocks := params.EpochBlocks
	if epochBlocks == 0 {
		epochBlocks = 17280 // Default
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

	// Clear transition state
	if err := k.SeasonTransitionState.Remove(ctx); err != nil {
		return err
	}

	// Clear next season info
	_ = k.NextSeasonInfo.Remove(ctx)

	// Clear recovery state if any
	_ = k.TransitionRecoveryState.Remove(ctx)

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
	params, _ := k.Params.Get(ctx)
	maxRetries := params.TransitionMaxRetries
	if maxRetries == 0 {
		maxRetries = 3 // Default
	}

	if recovery.FailureCount >= uint64(maxRetries) {
		recovery.RecoveryMode = true
	}

	_ = k.TransitionRecoveryState.Set(ctx, recovery)

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
func (k Keeper) processExpiredModerations(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return
	}

	iter, err := k.DisplayNameModeration.Iterate(ctx, nil)
	if err != nil {
		return
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}

		moderation := kv.Value
		member := kv.Key

		// Skip inactive moderations
		if !moderation.Active {
			continue
		}

		// Skip moderations that have an appeal (those are resolved via ResolveDisplayNameAppeal)
		if moderation.AppealChallengeId != "" {
			continue
		}

		// Check if appeal period has expired
		deadline := moderation.ModeratedAt + int64(params.DisplayNameAppealPeriodBlocks)
		if currentBlock <= deadline {
			continue
		}

		// Auto-resolve: report upheld, unlock reporter's stake
		if err := k.ResolveUnappealedModerationInternal(ctx, member); err != nil {
			// Log error but don't fail the block
			sdkCtx.Logger().Error("failed to auto-resolve expired moderation",
				"member", member, "error", err)
		}
	}
}
