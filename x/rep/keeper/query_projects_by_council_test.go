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

func TestProjectsByCouncil(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*fixture)
		council    string
		wantID     uint64
		wantName   string
		wantStatus uint64
		wantErr    error
	}{
		{
			name: "ReturnsFirstProjectForCouncil",
			setup: func(f *fixture) {
				createProjectForCouncil(f.keeper, f.ctx, 1, "commons", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
				createProjectForCouncil(f.keeper, f.ctx, 2, "technical", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
				createProjectForCouncil(f.keeper, f.ctx, 3, "commons", types.ProjectStatus_PROJECT_STATUS_ACTIVE)
			},
			council:    "commons",
			wantID:     1,
			wantName:   "Project B1",
			wantStatus: uint64(types.ProjectStatus_PROJECT_STATUS_PROPOSED),
		},
		{
			name: "EmptyResponseWhenNoProjectsForCouncil",
			setup: func(f *fixture) {
				createProjectForCouncil(f.keeper, f.ctx, 1, "commons", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
				createProjectForCouncil(f.keeper, f.ctx, 2, "technical", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
			},
			council: "nonexistent",
			wantErr: nil,
		},
		{
			name:    "EmptyResponseWhenNoProjectsExist",
			setup:   func(f *fixture) {},
			council: "commons",
			wantErr: nil,
		},
		{
			name: "ReturnsProjectForTechnicalCouncil",
			setup: func(f *fixture) {
				createProjectForCouncil(f.keeper, f.ctx, 1, "technical", types.ProjectStatus_PROJECT_STATUS_ACTIVE)
				createProjectForCouncil(f.keeper, f.ctx, 2, "technical", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
			},
			council:    "technical",
			wantID:     1,
			wantName:   "Project B1",
			wantStatus: uint64(types.ProjectStatus_PROJECT_STATUS_ACTIVE),
		},
		{
			name:    "InvalidRequestNil",
			setup:   func(f *fixture) {},
			council: "",
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

			var req *types.QueryProjectsByCouncilRequest
			if tc.council != "" || tc.wantErr == nil {
				req = &types.QueryProjectsByCouncilRequest{Council: tc.council}
			}

			response, err := qs.ProjectsByCouncil(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantID > 0 {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantID, response.ProjectId)
				require.Equal(t, tc.wantName, response.Name)
				require.Equal(t, tc.wantStatus, response.Status)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, uint64(0), response.ProjectId)
				require.Equal(t, "", response.Name)
				require.Equal(t, uint64(0), response.Status)
			}
		})
	}
}

func TestProjectsByCouncil_MultipleProjects(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple projects for the same council
	createProjectForCouncil(f.keeper, f.ctx, 1, "commons", types.ProjectStatus_PROJECT_STATUS_PROPOSED)
	createProjectForCouncil(f.keeper, f.ctx, 2, "commons", types.ProjectStatus_PROJECT_STATUS_ACTIVE)
	createProjectForCouncil(f.keeper, f.ctx, 3, "commons", types.ProjectStatus_PROJECT_STATUS_COMPLETED)

	// Query should return first project (id 1)
	response, err := qs.ProjectsByCouncil(f.ctx, &types.QueryProjectsByCouncilRequest{Council: "commons"})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, uint64(1), response.ProjectId)
	require.Equal(t, "Project B1", response.Name)
	require.Equal(t, uint64(types.ProjectStatus_PROJECT_STATUS_PROPOSED), response.Status)
}
