package keeper

import (
	"context"

	"sparkdream/x/season/types"
)

// BootstrapTitlesAndAchievements initializes sample titles and achievements for testing/development.
// This data should be replaced with production values before mainnet launch.
//
// Design Notes:
// - Achievements reward specific milestones (completed X things, earned Y reputation)
// - Titles are prestigious designations (both seasonal and permanent)
// - XP rewards follow rarity: Common=100-250, Uncommon=250-500, Rare=500-1000, Epic=1000-2500, Legendary=2500+
func (k Keeper) BootstrapTitlesAndAchievements(ctx context.Context) {
	// =========================================================================
	// ACHIEVEMENTS - Specific milestones that grant XP
	// =========================================================================

	achievements := []types.Achievement{
		// --- COMMON ACHIEVEMENTS (First milestones, low XP) ---
		{
			AchievementId:        "first_step",
			Name:                 "First Step",
			Description:          "Successfully invited your first member to the commons",
			Rarity:               types.Rarity_RARITY_COMMON,
			XpReward:             100,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INVITATIONS_SUCCESSFUL,
			RequirementThreshold: 1,
		},
		{
			AchievementId:        "contributor",
			Name:                 "Contributor",
			Description:          "Completed your first initiative",
			Rarity:               types.Rarity_RARITY_COMMON,
			XpReward:             150,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED,
			RequirementThreshold: 1,
		},
		{
			AchievementId:        "voice_heard",
			Name:                 "Voice Heard",
			Description:          "Cast your first vote in governance",
			Rarity:               types.Rarity_RARITY_COMMON,
			XpReward:             100,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_VOTES_CAST,
			RequirementThreshold: 1,
		},
		{
			AchievementId:        "helping_hand",
			Name:                 "Helping Hand",
			Description:          "Received 10 helpful votes in the forum",
			Rarity:               types.Rarity_RARITY_COMMON,
			XpReward:             200,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_FORUM_HELPFUL,
			RequirementThreshold: 10,
		},

		// --- UNCOMMON ACHIEVEMENTS (Sustained engagement) ---
		{
			AchievementId:        "proven_contributor",
			Name:                 "Proven Contributor",
			Description:          "Completed 5 initiatives",
			Rarity:               types.Rarity_RARITY_UNCOMMON,
			XpReward:             400,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED,
			RequirementThreshold: 5,
		},
		{
			AchievementId:        "community_builder",
			Name:                 "Community Builder",
			Description:          "Successfully invited 5 members",
			Rarity:               types.Rarity_RARITY_UNCOMMON,
			XpReward:             350,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INVITATIONS_SUCCESSFUL,
			RequirementThreshold: 5,
		},
		{
			AchievementId:        "active_voter",
			Name:                 "Active Voter",
			Description:          "Cast votes in 10 governance proposals",
			Rarity:               types.Rarity_RARITY_UNCOMMON,
			XpReward:             300,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_VOTES_CAST,
			RequirementThreshold: 10,
		},
		{
			AchievementId:        "juror",
			Name:                 "Juror",
			Description:          "Completed 3 jury duties",
			Rarity:               types.Rarity_RARITY_UNCOMMON,
			XpReward:             400,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_JURY_DUTY,
			RequirementThreshold: 3,
		},

		// --- RARE ACHIEVEMENTS (Significant accomplishments) ---
		{
			AchievementId:        "veteran_contributor",
			Name:                 "Veteran Contributor",
			Description:          "Completed 20 initiatives",
			Rarity:               types.Rarity_RARITY_RARE,
			XpReward:             800,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED,
			RequirementThreshold: 20,
		},
		{
			AchievementId:        "talent_scout",
			Name:                 "Talent Scout",
			Description:          "Successfully invited 20 members",
			Rarity:               types.Rarity_RARITY_RARE,
			XpReward:             700,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INVITATIONS_SUCCESSFUL,
			RequirementThreshold: 20,
		},
		{
			AchievementId:        "forum_sage",
			Name:                 "Forum Sage",
			Description:          "Received 100 helpful votes in the forum",
			Rarity:               types.Rarity_RARITY_RARE,
			XpReward:             750,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_FORUM_HELPFUL,
			RequirementThreshold: 100,
		},
		{
			AchievementId:        "justice_keeper",
			Name:                 "Justice Keeper",
			Description:          "Completed 10 jury duties",
			Rarity:               types.Rarity_RARITY_RARE,
			XpReward:             900,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_JURY_DUTY,
			RequirementThreshold: 10,
		},
		{
			AchievementId:        "challenger",
			Name:                 "Challenger",
			Description:          "Won 5 challenge disputes",
			Rarity:               types.Rarity_RARITY_RARE,
			XpReward:             850,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_CHALLENGES_WON,
			RequirementThreshold: 5,
		},

		// --- EPIC ACHIEVEMENTS (Exceptional dedication) ---
		{
			AchievementId:        "master_contributor",
			Name:                 "Master Contributor",
			Description:          "Completed 50 initiatives",
			Rarity:               types.Rarity_RARITY_EPIC,
			XpReward:             2000,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED,
			RequirementThreshold: 50,
		},
		{
			AchievementId:        "recruitment_champion",
			Name:                 "Recruitment Champion",
			Description:          "Successfully invited 50 members",
			Rarity:               types.Rarity_RARITY_EPIC,
			XpReward:             1800,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INVITATIONS_SUCCESSFUL,
			RequirementThreshold: 50,
		},
		{
			AchievementId:        "governance_pillar",
			Name:                 "Governance Pillar",
			Description:          "Cast votes in 50 governance proposals",
			Rarity:               types.Rarity_RARITY_EPIC,
			XpReward:             1500,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_VOTES_CAST,
			RequirementThreshold: 50,
		},
		{
			AchievementId:        "supreme_juror",
			Name:                 "Supreme Juror",
			Description:          "Completed 25 jury duties",
			Rarity:               types.Rarity_RARITY_EPIC,
			XpReward:             2200,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_JURY_DUTY,
			RequirementThreshold: 25,
		},

		// --- LEGENDARY ACHIEVEMENTS (Hall of fame level) ---
		{
			AchievementId:        "legendary_builder",
			Name:                 "Legendary Builder",
			Description:          "Completed 100 initiatives",
			Rarity:               types.Rarity_RARITY_LEGENDARY,
			XpReward:             5000,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED,
			RequirementThreshold: 100,
		},
		{
			AchievementId:        "community_founder",
			Name:                 "Community Founder",
			Description:          "Successfully invited 100 members",
			Rarity:               types.Rarity_RARITY_LEGENDARY,
			XpReward:             4500,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INVITATIONS_SUCCESSFUL,
			RequirementThreshold: 100,
		},
		{
			AchievementId:        "eternal_veteran",
			Name:                 "Eternal Veteran",
			Description:          "Active for 10 seasons",
			Rarity:               types.Rarity_RARITY_LEGENDARY,
			XpReward:             6000,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE,
			RequirementThreshold: 10,
		},

		// --- UNIQUE ACHIEVEMENTS (One-of-a-kind) ---
		{
			AchievementId:        "founding_member",
			Name:                 "Founding Member",
			Description:          "Among the first to join the commons",
			Rarity:               types.Rarity_RARITY_UNIQUE,
			XpReward:             10000,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE,
			RequirementThreshold: 1,
		},
	}

	// Store all achievements
	for _, achievement := range achievements {
		_ = k.Achievement.Set(ctx, achievement.AchievementId, achievement)
	}

	// =========================================================================
	// TITLES - Prestigious designations (both seasonal and permanent)
	// =========================================================================

	titles := []types.Title{
		// --- PERMANENT TITLES (Non-seasonal, earned once) ---
		{
			TitleId:              "newcomer",
			Name:                 "Newcomer",
			Description:          "New to the commons",
			Rarity:               types.Rarity_RARITY_COMMON,
			Seasonal:             false,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE,
			RequirementThreshold: 1,
		},
		{
			TitleId:              "veteran",
			Name:                 "Veteran",
			Description:          "Seasoned member of the commons",
			Rarity:               types.Rarity_RARITY_UNCOMMON,
			Seasonal:             false,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE,
			RequirementThreshold: 3,
		},
		{
			TitleId:              "elder",
			Name:                 "Elder",
			Description:          "Long-standing pillar of the community",
			Rarity:               types.Rarity_RARITY_RARE,
			Seasonal:             false,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE,
			RequirementThreshold: 5,
		},
		{
			TitleId:              "sage",
			Name:                 "Sage",
			Description:          "Wise counsel, respected voice",
			Rarity:               types.Rarity_RARITY_EPIC,
			Seasonal:             false,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_FORUM_HELPFUL,
			RequirementThreshold: 500,
		},
		{
			TitleId:              "architect",
			Name:                 "Architect",
			Description:          "Master builder of the commons",
			Rarity:               types.Rarity_RARITY_LEGENDARY,
			Seasonal:             false,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED,
			RequirementThreshold: 100,
		},

		// --- SEASONAL TITLES (Earned each season with "S{N} " prefix) ---
		{
			TitleId:              "champion",
			Name:                 "Champion",
			Description:          "Top 10 XP earner in the season",
			Rarity:               types.Rarity_RARITY_EPIC,
			Seasonal:             true,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_TOP_XP,
			RequirementThreshold: 10, // Top 10
		},
		{
			TitleId:              "rising_star",
			Name:                 "Rising Star",
			Description:          "Reached level 5 this season",
			Rarity:               types.Rarity_RARITY_UNCOMMON,
			Seasonal:             true,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL,
			RequirementThreshold: 5,
		},
		{
			TitleId:              "exemplar",
			Name:                 "Exemplar",
			Description:          "Reached level 10 this season",
			Rarity:               types.Rarity_RARITY_RARE,
			Seasonal:             true,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL,
			RequirementThreshold: 10,
		},
		{
			TitleId:              "legend",
			Name:                 "Legend",
			Description:          "Reached level 20 this season",
			Rarity:               types.Rarity_RARITY_LEGENDARY,
			Seasonal:             true,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL,
			RequirementThreshold: 20,
		},
		{
			TitleId:              "achiever",
			Name:                 "Achiever",
			Description:          "Unlocked 10 achievements this season",
			Rarity:               types.Rarity_RARITY_RARE,
			Seasonal:             true,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_ACHIEVEMENT_COUNT,
			RequirementThreshold: 10,
		},
		{
			TitleId:              "guardian",
			Name:                 "Guardian",
			Description:          "Won 3 challenges this season",
			Rarity:               types.Rarity_RARITY_EPIC,
			Seasonal:             true,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_CHALLENGES_WON,
			RequirementThreshold: 3,
		},
	}

	// Store all titles
	for _, title := range titles {
		_ = k.Title.Set(ctx, title.TitleId, title)
	}
}
