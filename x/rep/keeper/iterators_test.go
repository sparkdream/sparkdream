package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestIterateActiveInitiatives tests iteration over active initiatives
func TestIterateActiveInitiatives(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member for initiatives
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "50.0"},
	})

	// Create initiatives with all possible statuses
	testCases := []struct {
		id            uint64
		status        types.InitiativeStatus
		shouldIterate bool
	}{
		{0, types.InitiativeStatus_INITIATIVE_STATUS_OPEN, true},
		{1, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, true},
		{2, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, true},
		{3, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, true},
		{4, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, true},
		{5, types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, false},
		{6, types.InitiativeStatus_INITIATIVE_STATUS_REJECTED, false},
		{7, types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED, false},
	}

	for _, tc := range testCases {
		initiative := types.Initiative{
			Id:          tc.id,
			ProjectId:   1,
			Title:       "Test Initiative",
			Description: "Test",
			Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			Status:      tc.status,
			Budget:      PtrInt(math.NewInt(100)),
		}
		err := k.Initiative.Set(ctx, tc.id, initiative)
		require.NoError(t, err)
		// Add to status index for the iterator to find it
		err = k.AddInitiativeToStatusIndex(ctx, initiative)
		require.NoError(t, err)
	}

	// Iterate and collect IDs
	var collected []uint64
	k.IterateActiveInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		collected = append(collected, initiative.Id)
		return false // Don't stop early
	})

	// Verify only active initiatives were collected
	require.Len(t, collected, 5, "Should iterate exactly 5 active initiatives")

	// Check that all active statuses are included
	for _, tc := range testCases {
		if tc.shouldIterate {
			require.Contains(t, collected, tc.id, "Should include initiative with status %s", tc.status)
		} else {
			require.NotContains(t, collected, tc.id, "Should not include initiative with status %s", tc.status)
		}
	}
}

// TestIterateActiveInitiatives_EarlyStop tests early termination
func TestIterateActiveInitiatives_EarlyStop(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "50.0"},
	})

	// Create 10 open initiatives
	for i := 0; i < 10; i++ {
		initiative := types.Initiative{
			Id:          uint64(i),
			ProjectId:   1,
			Title:       "Test Initiative",
			Description: "Test",
			Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			Status:      types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
			Budget:      PtrInt(math.NewInt(100)),
		}
		k.Initiative.Set(ctx, uint64(i), initiative)
		k.AddInitiativeToStatusIndex(ctx, initiative)
	}

	// Test early stop after 3 iterations
	count := 0
	k.IterateActiveInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		count++
		return count >= 3 // Stop after 3
	})

	require.Equal(t, 3, count, "Should stop early after 3 iterations")
}

// TestIterateActiveInitiatives_EmptyCollection tests with no initiatives
func TestIterateActiveInitiatives_EmptyCollection(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Test with no initiatives
	called := false
	k.IterateActiveInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		called = true
		return false
	})

	require.False(t, called, "Should not call function when no initiatives exist")
}

// TestIterateSubmittedInitiatives tests iteration over submitted initiatives
func TestIterateSubmittedInitiatives(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "50.0"},
	})

	// Create initiatives with different statuses
	statuses := []struct {
		id            uint64
		status        types.InitiativeStatus
		shouldIterate bool
	}{
		{0, types.InitiativeStatus_INITIATIVE_STATUS_OPEN, false},
		{1, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, true},
		{2, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, true},
		{3, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, false},
		{4, types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, false},
	}

	for _, s := range statuses {
		initiative := types.Initiative{
			Id:          s.id,
			ProjectId:   1,
			Title:       "Test Initiative",
			Description: "Test",
			Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			Status:      s.status,
			Budget:      PtrInt(math.NewInt(100)),
		}
		k.Initiative.Set(ctx, s.id, initiative)
		k.AddInitiativeToStatusIndex(ctx, initiative)
	}

	// Iterate and collect
	var collected []uint64
	k.IterateSubmittedInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		collected = append(collected, initiative.Id)
		return false
	})

	// Verify only submitted initiatives
	require.Len(t, collected, 2, "Should iterate exactly 2 submitted initiatives")
	require.Contains(t, collected, uint64(1))
	require.Contains(t, collected, uint64(2))
	require.NotContains(t, collected, uint64(0))
	require.NotContains(t, collected, uint64(3))
	require.NotContains(t, collected, uint64(4))
}

// TestIterateSubmittedInitiatives_EarlyStop tests early termination
func TestIterateSubmittedInitiatives_EarlyStop(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "50.0"},
	})

	// Create 5 submitted initiatives
	for i := 0; i < 5; i++ {
		initiative := types.Initiative{
			Id:          uint64(i),
			ProjectId:   1,
			Title:       "Test Initiative",
			Description: "Test",
			Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			Status:      types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
			Budget:      PtrInt(math.NewInt(100)),
		}
		k.Initiative.Set(ctx, uint64(i), initiative)
		k.AddInitiativeToStatusIndex(ctx, initiative)
	}

	// Test early stop after 2
	count := 0
	k.IterateSubmittedInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		count++
		return count >= 2
	})

	require.Equal(t, 2, count, "Should stop early after 2 iterations")
}

// TestIterateSubmittedInitiatives_EmptyCollection tests with no submitted initiatives
func TestIterateSubmittedInitiatives_EmptyCollection(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	called := false
	k.IterateSubmittedInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		called = true
		return false
	})

	require.False(t, called, "Should not call function when no submitted initiatives exist")
}

// TestIteratePendingCompletionInitiatives tests iteration over in-review initiatives
func TestIteratePendingCompletionInitiatives(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "50.0"},
	})

	// Create initiatives with different statuses
	statuses := []struct {
		id            uint64
		status        types.InitiativeStatus
		shouldIterate bool
	}{
		{0, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, false},
		{1, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, true},
		{2, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, true},
		{3, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, false},
		{4, types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, false},
	}

	for _, s := range statuses {
		initiative := types.Initiative{
			Id:          s.id,
			ProjectId:   1,
			Title:       "Test Initiative",
			Description: "Test",
			Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			Status:      s.status,
			Budget:      PtrInt(math.NewInt(100)),
		}
		k.Initiative.Set(ctx, s.id, initiative)
		k.AddInitiativeToStatusIndex(ctx, initiative)
	}

	// Iterate and collect
	var collected []uint64
	k.IteratePendingCompletionInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		collected = append(collected, initiative.Id)
		return false
	})

	// Verify only in-review initiatives
	require.Len(t, collected, 2, "Should iterate exactly 2 in-review initiatives")
	require.Contains(t, collected, uint64(1))
	require.Contains(t, collected, uint64(2))
	require.NotContains(t, collected, uint64(0))
	require.NotContains(t, collected, uint64(3))
	require.NotContains(t, collected, uint64(4))
}

// TestIteratePendingCompletionInitiatives_EarlyStop tests early termination
func TestIteratePendingCompletionInitiatives_EarlyStop(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "50.0"},
	})

	// Create 4 in-review initiatives
	for i := 0; i < 4; i++ {
		initiative := types.Initiative{
			Id:          uint64(i),
			ProjectId:   1,
			Title:       "Test Initiative",
			Description: "Test",
			Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			Status:      types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW,
			Budget:      PtrInt(math.NewInt(100)),
		}
		k.Initiative.Set(ctx, uint64(i), initiative)
		k.AddInitiativeToStatusIndex(ctx, initiative)
	}

	// Test early stop
	count := 0
	k.IteratePendingCompletionInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		count++
		return count >= 1
	})

	require.Equal(t, 1, count, "Should stop early after 1 iteration")
}

// TestIteratePendingCompletionInitiatives_EmptyCollection tests with no in-review initiatives
func TestIteratePendingCompletionInitiatives_EmptyCollection(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	called := false
	k.IteratePendingCompletionInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		called = true
		return false
	})

	require.False(t, called, "Should not call function when no in-review initiatives exist")
}

// TestIterateActiveJuryReviews tests iteration over pending jury reviews
func TestIterateActiveJuryReviews(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create jury reviews with different verdicts
	verdicts := []struct {
		id            uint64
		verdict       types.Verdict
		shouldIterate bool
	}{
		{0, types.Verdict_VERDICT_PENDING, true},
		{1, types.Verdict_VERDICT_PENDING, true},
		{2, types.Verdict_VERDICT_UPHOLD_CHALLENGE, false},
		{3, types.Verdict_VERDICT_REJECT_CHALLENGE, false},
	}

	for _, v := range verdicts {
		review := types.JuryReview{
			Id:      v.id,
			Verdict: v.verdict,
		}
		k.JuryReview.Set(ctx, v.id, review)
		k.AddJuryReviewToVerdictIndex(ctx, review)
	}

	// Iterate and collect
	var collected []uint64
	k.IterateActiveJuryReviews(ctx, func(index int64, review types.JuryReview) bool {
		collected = append(collected, review.Id)
		return false
	})

	// Verify only pending reviews
	require.Len(t, collected, 2, "Should iterate exactly 2 pending jury reviews")
	require.Contains(t, collected, uint64(0))
	require.Contains(t, collected, uint64(1))
	require.NotContains(t, collected, uint64(2))
	require.NotContains(t, collected, uint64(3))
}

// TestIterateActiveJuryReviews_EarlyStop tests early termination
func TestIterateActiveJuryReviews_EarlyStop(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create 5 pending reviews
	for i := 0; i < 5; i++ {
		review := types.JuryReview{
			Id:      uint64(i),
			Verdict: types.Verdict_VERDICT_PENDING,
		}
		k.JuryReview.Set(ctx, uint64(i), review)
		k.AddJuryReviewToVerdictIndex(ctx, review)
	}

	// Test early stop
	count := 0
	k.IterateActiveJuryReviews(ctx, func(index int64, review types.JuryReview) bool {
		count++
		return count >= 3
	})

	require.Equal(t, 3, count, "Should stop early after 3 iterations")
}

// TestIterateActiveJuryReviews_EmptyCollection tests with no pending reviews
func TestIterateActiveJuryReviews_EmptyCollection(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	called := false
	k.IterateActiveJuryReviews(ctx, func(index int64, review types.JuryReview) bool {
		called = true
		return false
	})

	require.False(t, called, "Should not call function when no pending reviews exist")
}

// TestIteratePendingInterims tests iteration over pending/in-progress interims
func TestIteratePendingInterims(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create interims with different statuses
	statuses := []struct {
		id            uint64
		status        types.InterimStatus
		shouldIterate bool
	}{
		{0, types.InterimStatus_INTERIM_STATUS_PENDING, true},
		{1, types.InterimStatus_INTERIM_STATUS_IN_PROGRESS, true},
		{2, types.InterimStatus_INTERIM_STATUS_PENDING, true},
		{3, types.InterimStatus_INTERIM_STATUS_COMPLETED, false},
		{4, types.InterimStatus_INTERIM_STATUS_EXPIRED, false},
	}

	for _, s := range statuses {
		interim := types.Interim{
			Id:     s.id,
			Status: s.status,
			Type:   types.InterimType_INTERIM_TYPE_JURY_DUTY,
			Budget: PtrInt(math.NewInt(100)),
		}
		k.Interim.Set(ctx, s.id, interim)
		k.AddInterimToStatusIndex(ctx, interim)
	}

	// Iterate and collect
	var collected []uint64
	k.IteratePendingInterims(ctx, func(index int64, interim types.Interim) bool {
		collected = append(collected, interim.Id)
		return false
	})

	// Verify only pending/in-progress interims
	require.Len(t, collected, 3, "Should iterate exactly 3 pending/in-progress interims")
	require.Contains(t, collected, uint64(0))
	require.Contains(t, collected, uint64(1))
	require.Contains(t, collected, uint64(2))
	require.NotContains(t, collected, uint64(3))
	require.NotContains(t, collected, uint64(4))
}

// TestIteratePendingInterims_EarlyStop tests early termination
func TestIteratePendingInterims_EarlyStop(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create 6 pending interims
	for i := 0; i < 6; i++ {
		interim := types.Interim{
			Id:     uint64(i),
			Status: types.InterimStatus_INTERIM_STATUS_PENDING,
			Type:   types.InterimType_INTERIM_TYPE_JURY_DUTY,
			Budget: PtrInt(math.NewInt(100)),
		}
		k.Interim.Set(ctx, uint64(i), interim)
		k.AddInterimToStatusIndex(ctx, interim)
	}

	// Test early stop
	count := 0
	k.IteratePendingInterims(ctx, func(index int64, interim types.Interim) bool {
		count++
		return count >= 4
	})

	require.Equal(t, 4, count, "Should stop early after 4 iterations")
}

// TestIteratePendingInterims_EmptyCollection tests with no pending interims
func TestIteratePendingInterims_EmptyCollection(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	called := false
	k.IteratePendingInterims(ctx, func(index int64, interim types.Interim) bool {
		called = true
		return false
	})

	require.False(t, called, "Should not call function when no pending interims exist")
}

// TestIterators_MixedStatusLargeDataset tests iterators with large datasets
func TestIterators_MixedStatusLargeDataset(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "50.0"},
	})

	// Create 100 initiatives with mixed statuses
	activeCount := 0
	for i := 0; i < 100; i++ {
		// Alternate between active and inactive statuses
		var status types.InitiativeStatus
		if i%3 == 0 {
			status = types.InitiativeStatus_INITIATIVE_STATUS_OPEN
			activeCount++
		} else if i%3 == 1 {
			status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
			activeCount++
		} else {
			status = types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED
		}

		initiative := types.Initiative{
			Id:          uint64(i),
			ProjectId:   1,
			Title:       "Test Initiative",
			Description: "Test",
			Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			Status:      status,
			Budget:      PtrInt(math.NewInt(100)),
		}
		k.Initiative.Set(ctx, uint64(i), initiative)
		k.AddInitiativeToStatusIndex(ctx, initiative)
	}

	// Iterate and verify count
	count := 0
	k.IterateActiveInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		count++
		// Verify each initiative has an active status
		require.NotEqual(t, types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, initiative.Status)
		return false
	})

	require.Equal(t, activeCount, count, "Should iterate exactly %d active initiatives", activeCount)
}

// TestIterators_IndexConsistency tests that index parameter is consistent
func TestIterators_IndexConsistency(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create interims with specific IDs
	ids := []uint64{5, 10, 15, 20}
	for _, id := range ids {
		interim := types.Interim{
			Id:     id,
			Status: types.InterimStatus_INTERIM_STATUS_PENDING,
			Type:   types.InterimType_INTERIM_TYPE_JURY_DUTY,
			Budget: PtrInt(math.NewInt(100)),
		}
		k.Interim.Set(ctx, id, interim)
		k.AddInterimToStatusIndex(ctx, interim)
	}

	// Verify index matches ID
	k.IteratePendingInterims(ctx, func(index int64, interim types.Interim) bool {
		require.Equal(t, interim.Id, uint64(index), "Index should match interim ID")
		return false
	})
}
