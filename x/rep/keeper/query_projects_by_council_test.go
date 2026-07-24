package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createProjectForCouncil(k keeper.Keeper, ctx context.Context, id uint64, council string, status types.ProjectStatus) types.Project {
	amount := math.NewInt(1000000)
	project := types.Project{
		Id:             id,
		Name:           "Project " + string(rune('A'+id%26)) + string(rune('0'+id)),
		Description:    "Description for project " + string(rune('0'+id)),
		Creator:        "creator",
		Council:        council,
		Status:         status,
		ApprovedBudget: &amount,
		ApprovedSpark:  PtrInt(math.NewInt(100)),
	}
	_ = k.Project.Set(ctx, id, project)
	_ = k.ProjectSeq.Set(ctx, id)
	return project
}

func projectIDs(list []types.Project) []uint64 {
	ids := make([]uint64, len(list))
	for i, p := range list {
		ids[i] = p.Id
	}
	return ids
}

func TestProjectsByCouncil(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*fixture)
		request *types.QueryProjectsByCouncilRequest
		wantIDs []uint64
		wantErr error
	}{
		{
			name: "ReturnsEveryProjectForCouncil",
			setup: func(f *fixture) {
				createProjectForCouncil(f.keeper, f.ctx, 1, "commons", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
				createProjectForCouncil(f.keeper, f.ctx, 2, "technical", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
				createProjectForCouncil(f.keeper, f.ctx, 3, "commons", types.ProjectStatus_PROJECT_STATUS_ACTIVE)
			},
			request: &types.QueryProjectsByCouncilRequest{Council: "commons"},
			wantIDs: []uint64{1, 3},
		},
		{
			name: "EmptyWhenNoProjectsForCouncil",
			setup: func(f *fixture) {
				createProjectForCouncil(f.keeper, f.ctx, 1, "commons", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
				createProjectForCouncil(f.keeper, f.ctx, 2, "technical", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
			},
			request: &types.QueryProjectsByCouncilRequest{Council: "nonexistent"},
			wantIDs: nil,
		},
		{
			name:    "EmptyWhenNoProjectsExist",
			setup:   func(f *fixture) {},
			request: &types.QueryProjectsByCouncilRequest{Council: "commons"},
			wantIDs: nil,
		},
		{
			name: "SortByStatus",
			setup: func(f *fixture) {
				createProjectForCouncil(f.keeper, f.ctx, 1, "technical", types.ProjectStatus_PROJECT_STATUS_ACTIVE)
				createProjectForCouncil(f.keeper, f.ctx, 2, "technical", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
			},
			request: &types.QueryProjectsByCouncilRequest{Council: "technical", SortBy: "status"},
			wantIDs: []uint64{2, 1},
		},
		{
			name:    "UnknownSortKeyRejected",
			setup:   func(f *fixture) {},
			request: &types.QueryProjectsByCouncilRequest{Council: "commons", SortBy: "bogus"},
			wantErr: status.Error(codes.InvalidArgument, `unknown sort_by "bogus" (want id, name, budget or status)`),
		},
		{
			name:    "EmptyCouncilRejected",
			setup:   func(f *fixture) {},
			request: &types.QueryProjectsByCouncilRequest{Council: ""},
			wantErr: status.Error(codes.InvalidArgument, "council cannot be empty"),
		},
		{
			name:    "InvalidRequestNil",
			setup:   func(f *fixture) {},
			request: nil,
			wantErr: status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			response, err := qs.ProjectsByCouncil(f.ctx, tc.request)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, response)
			if len(tc.wantIDs) == 0 {
				require.Empty(t, response.Projects)
			} else {
				require.Equal(t, tc.wantIDs, projectIDs(response.Projects))
			}
			require.NotNil(t, response.Pagination)
			require.Equal(t, uint64(len(tc.wantIDs)), response.Pagination.Total)
		})
	}
}

func TestProjectsByCouncil_MultipleProjects(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	createProjectForCouncil(f.keeper, f.ctx, 1, "commons", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
	createProjectForCouncil(f.keeper, f.ctx, 2, "commons", types.ProjectStatus_PROJECT_STATUS_ACTIVE)
	createProjectForCouncil(f.keeper, f.ctx, 3, "commons", types.ProjectStatus_PROJECT_STATUS_COMPLETED)
	createProjectForCouncil(f.keeper, f.ctx, 4, "other", types.ProjectStatus_PROJECT_STATUS_ACTIVE)

	response, err := qs.ProjectsByCouncil(f.ctx, &types.QueryProjectsByCouncilRequest{Council: "commons"})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, []uint64{1, 2, 3}, projectIDs(response.Projects))
	require.Equal(t, uint64(3), response.Pagination.Total)
}
