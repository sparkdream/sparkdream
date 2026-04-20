package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/season/types"
)

func TestBootstrapTitlesAndAchievements(t *testing.T) {
	f := initFixture(t)

	f.keeper.BootstrapTitlesAndAchievements(f.ctx)

	// Verify a sampling of seeded achievements across rarities are persisted
	// with the expected metadata.
	expectedAchievements := map[string]struct {
		rarity    types.Rarity
		xpReward  uint64
		reqType   types.RequirementType
		threshold uint64
	}{
		"first_step":        {types.Rarity_RARITY_COMMON, 100, types.RequirementType_REQUIREMENT_TYPE_INVITATIONS_SUCCESSFUL, 1},
		"proven_contributor": {types.Rarity_RARITY_UNCOMMON, 400, types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED, 5},
		"veteran_contributor": {types.Rarity_RARITY_RARE, 800, types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED, 20},
		"master_contributor":  {types.Rarity_RARITY_EPIC, 2000, types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED, 50},
		"legendary_builder":   {types.Rarity_RARITY_LEGENDARY, 5000, types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED, 100},
		"first_season":        {types.Rarity_RARITY_UNIQUE, 1000, types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE, 1},
		"genesis_founder":     {types.Rarity_RARITY_UNIQUE, 10000, types.RequirementType_REQUIREMENT_TYPE_GENESIS, 0},
		"first_spark":         {types.Rarity_RARITY_UNIQUE, 15000, types.RequirementType_REQUIREMENT_TYPE_GENESIS, 0},
	}

	for id, expected := range expectedAchievements {
		got, err := f.keeper.Achievement.Get(f.ctx, id)
		require.NoError(t, err, "achievement %q should be stored", id)
		require.Equal(t, id, got.AchievementId)
		require.Equal(t, expected.rarity, got.Rarity)
		require.Equal(t, expected.xpReward, got.XpReward)
		require.Equal(t, expected.reqType, got.RequirementType)
		require.Equal(t, expected.threshold, got.RequirementThreshold)
	}

	// Verify a sampling of seeded titles, covering both seasonal and permanent.
	expectedTitles := map[string]struct {
		rarity   types.Rarity
		seasonal bool
		reqType  types.RequirementType
	}{
		"newcomer":     {types.Rarity_RARITY_COMMON, false, types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE},
		"architect":    {types.Rarity_RARITY_LEGENDARY, false, types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED},
		"champion":     {types.Rarity_RARITY_EPIC, true, types.RequirementType_REQUIREMENT_TYPE_TOP_XP},
		"legend":       {types.Rarity_RARITY_LEGENDARY, true, types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL},
		"rising_star":  {types.Rarity_RARITY_UNCOMMON, true, types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL},
	}

	for id, expected := range expectedTitles {
		got, err := f.keeper.Title.Get(f.ctx, id)
		require.NoError(t, err, "title %q should be stored", id)
		require.Equal(t, id, got.TitleId)
		require.Equal(t, expected.rarity, got.Rarity)
		require.Equal(t, expected.seasonal, got.Seasonal)
		require.Equal(t, expected.reqType, got.RequirementType)
	}

	// Bootstrap should be idempotent — calling it a second time must not error.
	f.keeper.BootstrapTitlesAndAchievements(f.ctx)

	got, err := f.keeper.Achievement.Get(f.ctx, "first_step")
	require.NoError(t, err)
	require.Equal(t, "first_step", got.AchievementId)
}
