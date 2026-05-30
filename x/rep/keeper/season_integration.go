package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetReputationScores returns all reputation scores for a member (tag -> score string).
// This is used by x/season during season transitions to snapshot reputation.
func (k Keeper) GetReputationScores(ctx context.Context, addr string) (map[string]string, error) {
	member, err := k.Member.Get(ctx, addr)
	if err != nil {
		return nil, types.ErrMemberNotFound
	}

	// Return a copy of the reputation scores map
	scores := make(map[string]string)
	for tag, score := range member.ReputationScores {
		scores[tag] = score
	}

	return scores, nil
}

// ArchiveSeasonalReputation archives the member's seasonal reputation to lifetime
// and resets the seasonal scores. Returns the archived reputation scores.
// This is called by x/season during the ARCHIVE_REPUTATION and RESET_REPUTATION phases.
func (k Keeper) ArchiveSeasonalReputation(ctx context.Context, addr string) (map[string]string, error) {
	member, err := k.Member.Get(ctx, addr)
	if err != nil {
		return nil, types.ErrMemberNotFound
	}

	// Initialize lifetime reputation if nil
	if member.LifetimeReputation == nil {
		member.LifetimeReputation = make(map[string]string)
	}

	// Save the current seasonal scores before archiving
	archivedScores := make(map[string]string)
	for tag, score := range member.ReputationScores {
		archivedScores[tag] = score
	}

	// Archive: Add seasonal scores to lifetime totals
	for tag, scoreStr := range member.ReputationScores {
		seasonScore, err := math.LegacyNewDecFromStr(scoreStr)
		if err != nil {
			continue // Skip malformed values
		}

		// Get existing lifetime score for this tag
		lifetimeScore := math.LegacyZeroDec()
		if existingStr, ok := member.LifetimeReputation[tag]; ok {
			if parsed, err := math.LegacyNewDecFromStr(existingStr); err == nil {
				lifetimeScore = parsed
			}
		}

		// Add seasonal score to lifetime
		newLifetime := lifetimeScore.Add(seasonScore)
		member.LifetimeReputation[tag] = newLifetime.String()
	}

	// Reset seasonal scores (clear for new season)
	member.ReputationScores = make(map[string]string)

	// Forum rep follows the same archive-then-reset pattern but writes to a
	// separate lifetime field. lifetime_forum_rep_per_tag is display-only —
	// the trust-level ladder never reads it, so a member who racked up
	// forum rep in season 1 cannot ride that signal into TRUSTED in
	// season 5 (which is the asymmetry the cap encodes).
	if member.LifetimeForumRepPerTag == nil {
		member.LifetimeForumRepPerTag = make(map[string]string)
	}
	for tag, scoreStr := range member.ForumRepPerTag {
		seasonScore, err := math.LegacyNewDecFromStr(scoreStr)
		if err != nil {
			continue
		}
		lifetimeScore := math.LegacyZeroDec()
		if existingStr, ok := member.LifetimeForumRepPerTag[tag]; ok {
			if parsed, err := math.LegacyNewDecFromStr(existingStr); err == nil {
				lifetimeScore = parsed
			}
		}
		member.LifetimeForumRepPerTag[tag] = lifetimeScore.Add(seasonScore).String()
	}
	forumTagsArchived := len(member.ForumRepPerTag)
	member.ForumRepPerTag = make(map[string]string)

	// Save updated member
	if err := k.Member.Set(ctx, addr, member); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"reputation_archived",
			sdk.NewAttribute("member", addr),
			sdk.NewAttribute("tags_archived", formatTagCount(len(archivedScores))),
			sdk.NewAttribute("forum_tags_archived", formatTagCount(forumTagsArchived)),
		),
	)

	return archivedScores, nil
}

// GetCompletedInitiativesCount returns the cached count of completed initiatives for a member.
// This is used by x/season for snapshot data.
func (k Keeper) GetCompletedInitiativesCount(ctx context.Context, addr string) (uint64, error) {
	member, err := k.Member.Get(ctx, addr)
	if err != nil {
		return 0, types.ErrMemberNotFound
	}

	return uint64(member.CompletedInitiativesCount), nil
}

// formatTagCount returns a string representation of the tag count
func formatTagCount(count int) string {
	return fmt.Sprintf("%d", count)
}
