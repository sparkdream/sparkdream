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

func createInitiativeForAssignee(k keeper.Keeper, ctx context.Context, id uint64, assignee string, status types.InitiativeStatus) types.Initiative {
	amount := math.NewInt(1000000)
	initiative := types.Initiative{
		Id:          id,
		ProjectId:   id,
		Title:       "Initiative " + strconv.FormatUint(id, 10),
		Description: "Description for initiative " + strconv.FormatUint(id, 10),
		Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		Category:    types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		TemplateId:  "template-" + strconv.FormatUint(id, 10),
		Budget:      &amount,
		Assignee:    assignee,
		Status:      status,
		CreatedAt:   1000,
	}
	_ = k.Initiative.Set(ctx, id, initiative)
	_ = k.InitiativeSeq.Set(ctx, id)
	return initiative
}

func TestInitiativesByAssignee(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*fixture)
		assignee   string
		wantID     uint64
		wantTitle  string
		wantStatus uint64
		wantErr    error
	}{
		{
			name: "ReturnsFirstInitiativeForAssignee",
			setup: func(f *fixture) {
				createInitiativeForAssignee(f.keeper, f.ctx, 1, "assignee1", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeForAssignee(f.keeper, f.ctx, 2, "assignee2", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeForAssignee(f.keeper, f.ctx, 3, "assignee1", types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
			},
			assignee:   "assignee1",
			wantID:     1,
			wantTitle:  "Initiative 1",
			wantStatus: uint64(types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED),
		},
		{
			name: "EmptyResponseWhenNoInitiativesForAssignee",
			setup: func(f *fixture) {
				createInitiativeForAssignee(f.keeper, f.ctx, 1, "assignee1", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeForAssignee(f.keeper, f.ctx, 2, "assignee2", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
			},
			assignee: "nonexistent",
			wantErr:  nil,
		},
		{
			name:     "EmptyResponseWhenNoInitiativesExist",
			setup:    func(f *fixture) {},
			assignee: "assignee1",
			wantErr:  nil,
		},
		{
			name: "ReturnsInitiativeWithSubmittedStatus",
			setup: func(f *fixture) {
				createInitiativeForAssignee(f.keeper, f.ctx, 1, "assigneeX", types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
				createInitiativeForAssignee(f.keeper, f.ctx, 2, "assigneeX", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
			},
			assignee:   "assigneeX",
			wantID:     1,
			wantTitle:  "Initiative 1",
			wantStatus: uint64(types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED),
		},
		{
			name:     "InvalidRequestNil",
			setup:    func(f *fixture) {},
			assignee: "",
			wantErr:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			var req *types.QueryInitiativesByAssigneeRequest
			if tc.assignee != "" || tc.wantErr == nil {
				req = &types.QueryInitiativesByAssigneeRequest{Assignee: tc.assignee}
			}

			response, err := qs.InitiativesByAssignee(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantID > 0 {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantID, response.InitiativeId)
				require.Equal(t, tc.wantTitle, response.Title)
				require.Equal(t, tc.wantStatus, response.Status)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, uint64(0), response.InitiativeId)
				require.Equal(t, "", response.Title)
				require.Equal(t, uint64(0), response.Status)
			}
		})
	}
}

func TestInitiativesByAssignee_MultipleInitiatives(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple initiatives for the same assignee
	createInitiativeForAssignee(f.keeper, f.ctx, 1, "worker1", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
	createInitiativeForAssignee(f.keeper, f.ctx, 2, "worker1", types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
	createInitiativeForAssignee(f.keeper, f.ctx, 3, "worker1", types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED)

	// Query should return first initiative (id 1)
	response, err := qs.InitiativesByAssignee(f.ctx, &types.QueryInitiativesByAssigneeRequest{Assignee: "worker1"})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, uint64(1), response.InitiativeId)
	require.Equal(t, "Initiative 1", response.Title)
	require.Equal(t, uint64(types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED), response.Status)
}
