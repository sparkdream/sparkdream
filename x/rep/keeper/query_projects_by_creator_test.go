package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createProjectForCreator(k keeper.Keeper, ctx context.Context, id uint64, creator string, status types.ProjectStatus) types.Project {
	amount := math.NewInt(1000000)
	project := types.Project{
		Id:             id,
		Name:           "Project " + strconv.FormatUint(id, 10),
		Description:    "Description for project " + strconv.FormatUint(id, 10),
		Creator:        creator,
		Council:        "commons",
		Status:         status,
		ApprovedBudget: &amount,
		ApprovedSpark:  PtrInt(math.NewInt(100)),
	}
	_ = k.Project.Set(ctx, id, project)
	_ = k.ProjectSeq.Set(ctx, id)
	return project
}

func TestProjectsByCreator(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*fixture)
		request *types.QueryProjectsByCreatorRequest
		wantIDs []uint64
		wantErr error
	}{
		{
			name: "ReturnsEveryProjectForCreator",
			setup: func(f *fixture) {
				createProjectForCreator(f.keeper, f.ctx, 1, "creator1", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
				createProjectForCreator(f.keeper, f.ctx, 2, "creator2", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
				createProjectForCreator(f.keeper, f.ctx, 3, "creator1", types.ProjectStatus_PROJECT_STATUS_ACTIVE)
			},
			request: &types.QueryProjectsByCreatorRequest{Creator: "creator1"},
			wantIDs: []uint64{1, 3},
		},
		{
			name: "EmptyWhenNoProjectsForCreator",
			setup: func(f *fixture) {
				createProjectForCreator(f.keeper, f.ctx, 1, "creator1", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
			},
			request: &types.QueryProjectsByCreatorRequest{Creator: "nonexistent"},
			wantIDs: nil,
		},
		{
			name:    "EmptyWhenNoProjectsExist",
			setup:   func(f *fixture) {},
			request: &types.QueryProjectsByCreatorRequest{Creator: "creator1"},
			wantIDs: nil,
		},
		{
			name: "SortByStatus",
			setup: func(f *fixture) {
				createProjectForCreator(f.keeper, f.ctx, 1, "creatorX", types.ProjectStatus_PROJECT_STATUS_ACTIVE)
				createProjectForCreator(f.keeper, f.ctx, 2, "creatorX", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
			},
			request: &types.QueryProjectsByCreatorRequest{Creator: "creatorX", SortBy: "status"},
			wantIDs: []uint64{2, 1},
		},
		{
			name: "UnknownSortKeyRejected",
			setup: func(f *fixture) {
				createProjectForCreator(f.keeper, f.ctx, 1, "creatorX", types.ProjectStatus_PROJECT_STATUS_ACTIVE)
			},
			request: &types.QueryProjectsByCreatorRequest{Creator: "creatorX", SortBy: "bogus"},
			wantErr: status.Error(codes.InvalidArgument, `unknown sort_by "bogus" (want id, name, budget or status)`),
		},
		{
			name:    "EmptyCreatorRejected",
			setup:   func(f *fixture) {},
			request: &types.QueryProjectsByCreatorRequest{Creator: ""},
			wantErr: status.Error(codes.InvalidArgument, "creator cannot be empty"),
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

			response, err := qs.ProjectsByCreator(f.ctx, tc.request)

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
