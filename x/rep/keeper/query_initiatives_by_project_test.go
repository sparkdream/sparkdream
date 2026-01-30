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

func createInitiativeForProject(k keeper.Keeper, ctx context.Context, id uint64, projectID uint64, status types.InitiativeStatus) types.Initiative {
	amount := math.NewInt(1000000)
	initiative := types.Initiative{
		Id:          id,
		ProjectId:   projectID,
		Title:       "Initiative " + strconv.FormatUint(id, 10),
		Description: "Description for initiative " + strconv.FormatUint(id, 10),
		Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		Category:    types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		TemplateId:  "template-" + strconv.FormatUint(id, 10),
		Budget:      &amount,
		Assignee:    "assignee",
		Status:      status,
		CreatedAt:   1000,
	}
	_ = k.Initiative.Set(ctx, id, initiative)
	_ = k.InitiativeSeq.Set(ctx, id)
	return initiative
}

func TestInitiativesByProject(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*fixture)
		projectID  uint64
		wantID     uint64
		wantTitle  string
		wantStatus uint64
		wantErr    error
	}{
		{
			name: "ReturnsFirstInitiativeForProject",
			setup: func(f *fixture) {
				createInitiativeForProject(f.keeper, f.ctx, 1, 10, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeForProject(f.keeper, f.ctx, 2, 20, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeForProject(f.keeper, f.ctx, 3, 10, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
			},
			projectID:  10,
			wantID:     1,
			wantTitle:  "Initiative 1",
			wantStatus: uint64(types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED),
		},
		{
			name: "EmptyResponseWhenNoInitiativesForProject",
			setup: func(f *fixture) {
				createInitiativeForProject(f.keeper, f.ctx, 1, 1, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeForProject(f.keeper, f.ctx, 2, 2, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
			},
			projectID: 99,
			wantErr:   nil,
		},
		{
			name:      "EmptyResponseWhenNoInitiativesExist",
			setup:     func(f *fixture) {},
			projectID: 1,
			wantErr:   nil,
		},
		{
			name: "ReturnsInitiativeWithCompletedStatus",
			setup: func(f *fixture) {
				createInitiativeForProject(f.keeper, f.ctx, 1, 50, types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED)
				createInitiativeForProject(f.keeper, f.ctx, 2, 50, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
			},
			projectID:  50,
			wantID:     1,
			wantTitle:  "Initiative 1",
			wantStatus: uint64(types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED),
		},
		{
			name:      "InvalidRequestNil",
			setup:     func(f *fixture) {},
			projectID: 0,
			wantErr:   status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			var req *types.QueryInitiativesByProjectRequest
			if tc.projectID > 0 || tc.wantErr == nil {
				req = &types.QueryInitiativesByProjectRequest{ProjectId: tc.projectID}
			}

			response, err := qs.InitiativesByProject(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantID > 0 {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.NotEmpty(t, response.Initiatives)
				require.Equal(t, tc.wantID, response.Initiatives[0].Id)
				require.Equal(t, tc.wantTitle, response.Initiatives[0].Title)
				require.Equal(t, types.InitiativeStatus(tc.wantStatus), response.Initiatives[0].Status)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Empty(t, response.Initiatives)
			}
		})
	}
}

func TestInitiativesByProject_MultipleInitiatives(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple initiatives for the same project
	createInitiativeForProject(f.keeper, f.ctx, 1, 100, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
	createInitiativeForProject(f.keeper, f.ctx, 2, 100, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
	createInitiativeForProject(f.keeper, f.ctx, 3, 100, types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED)

	// Query should return all initiatives for project 100
	response, err := qs.InitiativesByProject(f.ctx, &types.QueryInitiativesByProjectRequest{ProjectId: 100})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Len(t, response.Initiatives, 3)
	require.Equal(t, uint64(1), response.Initiatives[0].Id)
	require.Equal(t, "Initiative 1", response.Initiatives[0].Title)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, response.Initiatives[0].Status)
}
