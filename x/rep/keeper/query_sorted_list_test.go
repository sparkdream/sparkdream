package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func setSortTestProject(f *fixture, id uint64, name string, budget int64, status types.ProjectStatus) {
	amount := math.NewInt(budget)
	_ = f.keeper.Project.Set(f.ctx, id, types.Project{
		Id:             id,
		Name:           name,
		Creator:        "creator",
		Council:        "commons",
		Status:         status,
		ApprovedBudget: &amount,
	})
	_ = f.keeper.ProjectSeq.Set(f.ctx, id)
}

func setSortTestInitiative(f *fixture, id uint64, title string, budget int64, cur, req string) {
	amount := math.NewInt(budget)
	ini := types.Initiative{
		Id:        id,
		ProjectId: 1,
		Title:     title,
		Tier:      types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		Budget:    &amount,
		Status:    types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
	}
	if req != "" {
		r := math.LegacyMustNewDecFromStr(req)
		ini.RequiredConviction = &r
	}
	if cur != "" {
		c := math.LegacyMustNewDecFromStr(cur)
		ini.CurrentConviction = &c
	}
	_ = f.keeper.Initiative.Set(f.ctx, id, ini)
	_ = f.keeper.InitiativeSeq.Set(f.ctx, id)
}

func TestListProject_SortBy(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	setSortTestProject(f, 1, "banana", 300, types.ProjectStatus_PROJECT_STATUS_ACTIVE)
	setSortTestProject(f, 2, "Apple", 100, types.ProjectStatus_PROJECT_STATUS_PROPOSED)
	setSortTestProject(f, 3, "cherry", 200, types.ProjectStatus_PROJECT_STATUS_COMPLETED)

	// Name is case-insensitive: "Apple" < "banana" < "cherry".
	res, err := qs.ListProject(f.ctx, &types.QueryAllProjectRequest{SortBy: "name"})
	require.NoError(t, err)
	require.Equal(t, []uint64{2, 1, 3}, projectIDs(res.Project))

	res, err = qs.ListProject(f.ctx, &types.QueryAllProjectRequest{
		SortBy:     "name",
		Pagination: &query.PageRequest{Reverse: true},
	})
	require.NoError(t, err)
	require.Equal(t, []uint64{3, 1, 2}, projectIDs(res.Project))

	res, err = qs.ListProject(f.ctx, &types.QueryAllProjectRequest{
		SortBy:     "budget",
		Pagination: &query.PageRequest{Reverse: true},
	})
	require.NoError(t, err)
	require.Equal(t, []uint64{1, 3, 2}, projectIDs(res.Project))

	res, err = qs.ListProject(f.ctx, &types.QueryAllProjectRequest{SortBy: "status"})
	require.NoError(t, err)
	require.Equal(t, []uint64{2, 1, 3}, projectIDs(res.Project))

	// Without sort_by the key-paginated store path still serves the list.
	res, err = qs.ListProject(f.ctx, &types.QueryAllProjectRequest{})
	require.NoError(t, err)
	require.Equal(t, []uint64{1, 2, 3}, projectIDs(res.Project))
}

func TestListInitiative_SortBy(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// id 1: ratio 0.25; id 2: no required conviction (no ratio); id 3: ratio
	// 1.5 (overshoot ranks above met-exactly); id 4: ratio 1.0.
	setSortTestInitiative(f, 1, "delta", 400, "1.0", "4.0")
	setSortTestInitiative(f, 2, "alpha", 100, "", "")
	setSortTestInitiative(f, 3, "Charlie", 300, "3.0", "2.0")
	setSortTestInitiative(f, 4, "bravo", 200, "2.0", "2.0")

	// Closest-to-done first; the ratioless initiative sorts last regardless
	// of direction.
	res, err := qs.ListInitiative(f.ctx, &types.QueryAllInitiativeRequest{
		SortBy:     "conviction",
		Pagination: &query.PageRequest{Reverse: true},
	})
	require.NoError(t, err)
	require.Equal(t, []uint64{3, 4, 1, 2}, initiativeIDs(res.Initiative))

	res, err = qs.ListInitiative(f.ctx, &types.QueryAllInitiativeRequest{SortBy: "conviction"})
	require.NoError(t, err)
	require.Equal(t, []uint64{1, 4, 3, 2}, initiativeIDs(res.Initiative))

	res, err = qs.ListInitiative(f.ctx, &types.QueryAllInitiativeRequest{SortBy: "title"})
	require.NoError(t, err)
	require.Equal(t, []uint64{2, 4, 3, 1}, initiativeIDs(res.Initiative))

	res, err = qs.ListInitiative(f.ctx, &types.QueryAllInitiativeRequest{
		SortBy:     "budget",
		Pagination: &query.PageRequest{Reverse: true},
	})
	require.NoError(t, err)
	require.Equal(t, []uint64{1, 3, 4, 2}, initiativeIDs(res.Initiative))
}

func TestSortedPaginationEdgeCases(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	setSortTestProject(f, 1, "banana", 300, types.ProjectStatus_PROJECT_STATUS_ACTIVE)
	setSortTestProject(f, 2, "apple", 100, types.ProjectStatus_PROJECT_STATUS_PROPOSED)

	// A non-decimal pagination key (e.g. a store key from the unsorted path)
	// is rejected rather than silently treated as offset 0.
	_, err := qs.ListProject(f.ctx, &types.QueryAllProjectRequest{
		SortBy:     "name",
		Pagination: &query.PageRequest{Key: []byte{0x01, 0x02}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decimal offset")

	// An offset past the end yields an empty page, the true total, and no
	// next_key.
	res, err := qs.ListProject(f.ctx, &types.QueryAllProjectRequest{
		SortBy:     "name",
		Pagination: &query.PageRequest{Offset: 10},
	})
	require.NoError(t, err)
	require.Empty(t, res.Project)
	require.Equal(t, uint64(2), res.Pagination.Total)
	require.Empty(t, res.Pagination.NextKey)
}
