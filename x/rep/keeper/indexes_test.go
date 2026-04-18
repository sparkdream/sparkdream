package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// iterators_test.go already covers the IterateXxxByYyy helpers. These tests
// target the Add/Remove/Update mutations that feed those indexes — and
// specifically the invariants (no duplicate rows, status transitions clean
// up the old row, and Remove of an absent row is tolerated).

func collectStatuses(t *testing.T, f *fixture, statuses []types.InitiativeStatus) []uint64 {
	t.Helper()
	var ids []uint64
	require.NoError(t, f.keeper.IterateInitiativesByStatuses(f.ctx, statuses, func(id uint64, _ types.Initiative) bool {
		ids = append(ids, id)
		return false
	}))
	return ids
}

func TestInitiativeStatusIndex_AddRemoveUpdate(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	init := types.Initiative{Id: 42, Status: types.InitiativeStatus_INITIATIVE_STATUS_OPEN}
	require.NoError(t, k.Initiative.Set(ctx, init.Id, init))
	require.NoError(t, k.AddInitiativeToStatusIndex(ctx, init))

	ids := collectStatuses(t, f, []types.InitiativeStatus{types.InitiativeStatus_INITIATIVE_STATUS_OPEN})
	require.Equal(t, []uint64{42}, ids)

	// Transition OPEN -> ASSIGNED: old index row must disappear, new must appear.
	init.Status = types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED
	require.NoError(t, k.Initiative.Set(ctx, init.Id, init))
	require.NoError(t, k.UpdateInitiativeStatusIndex(ctx,
		types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
		types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, init.Id))

	require.Empty(t, collectStatuses(t, f, []types.InitiativeStatus{types.InitiativeStatus_INITIATIVE_STATUS_OPEN}))
	require.Equal(t, []uint64{42},
		collectStatuses(t, f, []types.InitiativeStatus{types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED}))

	// Same-status update is a no-op.
	require.NoError(t, k.UpdateInitiativeStatusIndex(ctx,
		types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED,
		types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, init.Id))
	require.Equal(t, []uint64{42},
		collectStatuses(t, f, []types.InitiativeStatus{types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED}))

	// Remove clears the index entirely.
	require.NoError(t, k.RemoveInitiativeFromStatusIndex(ctx, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, init.Id))
	require.Empty(t, collectStatuses(t, f, []types.InitiativeStatus{types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED}))
}

func TestInterimStatusIndex_AddRemoveUpdate(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	interim := types.Interim{Id: 7, Status: types.InterimStatus_INTERIM_STATUS_PENDING}
	require.NoError(t, k.Interim.Set(ctx, interim.Id, interim))
	require.NoError(t, k.AddInterimToStatusIndex(ctx, interim))

	var seen []uint64
	require.NoError(t, k.IterateInterimsByStatus(ctx, types.InterimStatus_INTERIM_STATUS_PENDING, func(id uint64) bool {
		seen = append(seen, id)
		return false
	}))
	require.Equal(t, []uint64{7}, seen)

	// PENDING -> IN_PROGRESS must drop the old index row.
	require.NoError(t, k.UpdateInterimStatusIndex(ctx,
		types.InterimStatus_INTERIM_STATUS_PENDING,
		types.InterimStatus_INTERIM_STATUS_IN_PROGRESS, interim.Id))

	seen = nil
	require.NoError(t, k.IterateInterimsByStatus(ctx, types.InterimStatus_INTERIM_STATUS_PENDING, func(id uint64) bool {
		seen = append(seen, id)
		return false
	}))
	require.Empty(t, seen)

	seen = nil
	require.NoError(t, k.IterateInterimsByStatus(ctx, types.InterimStatus_INTERIM_STATUS_IN_PROGRESS, func(id uint64) bool {
		seen = append(seen, id)
		return false
	}))
	require.Equal(t, []uint64{7}, seen)
}

func TestStakeTargetIndex_GetStakesByTarget(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Two stakes against the same initiative target; one against a different one.
	target := types.StakeTargetType_STAKE_TARGET_INITIATIVE
	stakes := []types.Stake{
		{Id: 1, TargetType: target, TargetId: 100},
		{Id: 2, TargetType: target, TargetId: 100},
		{Id: 3, TargetType: target, TargetId: 101},
	}
	for _, s := range stakes {
		require.NoError(t, k.Stake.Set(ctx, s.Id, s))
		require.NoError(t, k.AddStakeToTargetIndex(ctx, s))
	}

	got, err := k.GetStakesByTarget(ctx, target, 100)
	require.NoError(t, err)
	require.Len(t, got, 2)

	got, err = k.GetStakesByTarget(ctx, target, 101)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint64(3), got[0].Id)

	// Removing one stake from the index hides it from the target query.
	require.NoError(t, k.RemoveStakeFromTargetIndex(ctx, stakes[0]))
	got, err = k.GetStakesByTarget(ctx, target, 100)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint64(2), got[0].Id)
}

func TestChallengeStatusIndex_AddRemoveUpdate(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	ch := types.Challenge{Id: 11, Status: types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE}
	require.NoError(t, k.Challenge.Set(ctx, ch.Id, ch))
	require.NoError(t, k.AddChallengeToStatusIndex(ctx, ch))

	var seen []uint64
	require.NoError(t, k.IterateChallengesByStatus(ctx, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, func(id uint64, _ types.Challenge) bool {
		seen = append(seen, id)
		return false
	}))
	require.Equal(t, []uint64{11}, seen)

	// Transition ACTIVE -> UPHELD.
	require.NoError(t, k.UpdateChallengeStatusIndex(ctx,
		types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, ch.Id))

	seen = nil
	require.NoError(t, k.IterateChallengesByStatus(ctx, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, func(id uint64, _ types.Challenge) bool {
		seen = append(seen, id)
		return false
	}))
	require.Empty(t, seen)

	seen = nil
	require.NoError(t, k.IterateChallengesByStatus(ctx, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, func(id uint64, _ types.Challenge) bool {
		seen = append(seen, id)
		return false
	}))
	require.Equal(t, []uint64{11}, seen)
}

func TestJuryReviewVerdictIndex_AddUpdateRemove(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	jr := types.JuryReview{Id: 3, Verdict: types.Verdict_VERDICT_PENDING}
	require.NoError(t, k.JuryReview.Set(ctx, jr.Id, jr))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, jr))

	var seen []uint64
	require.NoError(t, k.IterateJuryReviewsByVerdict(ctx, types.Verdict_VERDICT_PENDING, func(id uint64, _ types.JuryReview) bool {
		seen = append(seen, id)
		return false
	}))
	require.Equal(t, []uint64{3}, seen)

	require.NoError(t, k.UpdateJuryReviewVerdictIndex(ctx, types.Verdict_VERDICT_PENDING, types.Verdict_VERDICT_UPHOLD_CHALLENGE, jr.Id))

	seen = nil
	require.NoError(t, k.IterateJuryReviewsByVerdict(ctx, types.Verdict_VERDICT_PENDING, func(id uint64, _ types.JuryReview) bool {
		seen = append(seen, id)
		return false
	}))
	require.Empty(t, seen)

	require.NoError(t, k.RemoveJuryReviewFromVerdictIndex(ctx, types.Verdict_VERDICT_UPHOLD_CHALLENGE, jr.Id))
	seen = nil
	require.NoError(t, k.IterateJuryReviewsByVerdict(ctx, types.Verdict_VERDICT_UPHOLD_CHALLENGE, func(id uint64, _ types.JuryReview) bool {
		seen = append(seen, id)
		return false
	}))
	require.Empty(t, seen)
}
