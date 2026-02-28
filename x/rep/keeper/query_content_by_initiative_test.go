package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// setupInitiativeForContentLink creates a project and initiative for testing content links.
func setupInitiativeForContentLink(t *testing.T, f *fixture) uint64 {
	t.Helper()

	creator := sdk.AccAddress([]byte("init_link_creator___"))
	f.keeper.Member.Set(f.ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag1": "100.0"},
	})
	f.keeper.MintDREAM(f.ctx, creator, math.NewInt(1000))

	projectID, err := f.keeper.CreateProject(
		f.ctx,
		sdk.AccAddress([]byte("proj_creator________")),
		"Test Project",
		"Description",
		[]string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(1000),
		math.NewInt(100),
	)
	require.NoError(t, err)
	f.keeper.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	initID, err := f.keeper.CreateInitiative(
		f.ctx,
		creator,
		projectID,
		"Test Initiative",
		"Desc",
		[]string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"",
		math.NewInt(100),
	)
	require.NoError(t, err)

	return initID
}

func TestQueryContentByInitiative_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.ContentByInitiative(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}

func TestQueryContentByInitiative_InitiativeNotFound(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.ContentByInitiative(f.ctx, &types.QueryContentByInitiativeRequest{
		InitiativeId: 999,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestQueryContentByInitiative_EmptyLinks(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	initID := setupInitiativeForContentLink(t, f)

	resp, err := qs.ContentByInitiative(f.ctx, &types.QueryContentByInitiativeRequest{
		InitiativeId: initID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.Links)
	require.True(t, resp.TotalPropagated.IsZero())
}

func TestQueryContentByInitiative_WithLinks(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	initID := setupInitiativeForContentLink(t, f)

	// Register content links
	err := f.keeper.RegisterContentInitiativeLink(f.ctx, initID, int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT), 1)
	require.NoError(t, err)
	err = f.keeper.RegisterContentInitiativeLink(f.ctx, initID, int32(types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT), 2)
	require.NoError(t, err)

	resp, err := qs.ContentByInitiative(f.ctx, &types.QueryContentByInitiativeRequest{
		InitiativeId: initID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Links, 2)

	// Links should have zero conviction since no stakes exist
	for _, link := range resp.Links {
		require.True(t, link.Conviction.IsZero())
	}
}

func TestQueryContentByInitiative_SingleLink(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	initID := setupInitiativeForContentLink(t, f)

	// Register a single content link
	err := f.keeper.RegisterContentInitiativeLink(f.ctx, initID, int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT), 42)
	require.NoError(t, err)

	resp, err := qs.ContentByInitiative(f.ctx, &types.QueryContentByInitiativeRequest{
		InitiativeId: initID,
	})
	require.NoError(t, err)
	require.Len(t, resp.Links, 1)
	require.Equal(t, int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT), resp.Links[0].TargetType)
	require.Equal(t, uint64(42), resp.Links[0].TargetId)
}
