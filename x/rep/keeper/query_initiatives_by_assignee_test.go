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
		name    string
		setup   func(*fixture)
		request *types.QueryInitiativesByAssigneeRequest
		wantIDs []uint64
		wantErr error
	}{
		{
			name: "ReturnsEveryInitiativeForAssignee",
			setup: func(f *fixture) {
				createInitiativeForAssignee(f.keeper, f.ctx, 1, "assignee1", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeForAssignee(f.keeper, f.ctx, 2, "assignee2", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeForAssignee(f.keeper, f.ctx, 3, "assignee1", types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
			},
			request: &types.QueryInitiativesByAssigneeRequest{Assignee: "assignee1"},
			wantIDs: []uint64{1, 3},
		},
		{
			name: "EmptyWhenNoInitiativesForAssignee",
			setup: func(f *fixture) {
				createInitiativeForAssignee(f.keeper, f.ctx, 1, "assignee1", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeForAssignee(f.keeper, f.ctx, 2, "assignee2", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
			},
			request: &types.QueryInitiativesByAssigneeRequest{Assignee: "nonexistent"},
			wantIDs: nil,
		},
		{
			name:    "EmptyWhenNoInitiativesExist",
			setup:   func(f *fixture) {},
			request: &types.QueryInitiativesByAssigneeRequest{Assignee: "assignee1"},
			wantIDs: nil,
		},
		{
			name: "SortByStatus",
			setup: func(f *fixture) {
				createInitiativeForAssignee(f.keeper, f.ctx, 1, "assigneeX", types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
				createInitiativeForAssignee(f.keeper, f.ctx, 2, "assigneeX", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
			},
			request: &types.QueryInitiativesByAssigneeRequest{Assignee: "assigneeX", SortBy: "status"},
			wantIDs: []uint64{2, 1},
		},
		{
			name:    "EmptyAssigneeRejected",
			setup:   func(f *fixture) {},
			request: &types.QueryInitiativesByAssigneeRequest{Assignee: ""},
			wantErr: status.Error(codes.InvalidArgument, "assignee cannot be empty"),
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

			response, err := qs.InitiativesByAssignee(f.ctx, tc.request)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, response)
			if len(tc.wantIDs) == 0 {
				require.Empty(t, response.Initiatives)
			} else {
				require.Equal(t, tc.wantIDs, initiativeIDs(response.Initiatives))
			}
			require.NotNil(t, response.Pagination)
			require.Equal(t, uint64(len(tc.wantIDs)), response.Pagination.Total)
		})
	}
}

func TestInitiativesByAssignee_MultipleInitiatives(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	createInitiativeForAssignee(f.keeper, f.ctx, 1, "worker1", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
	createInitiativeForAssignee(f.keeper, f.ctx, 2, "worker1", types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
	createInitiativeForAssignee(f.keeper, f.ctx, 3, "worker1", types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED)
	createInitiativeForAssignee(f.keeper, f.ctx, 4, "worker2", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)

	response, err := qs.InitiativesByAssignee(f.ctx, &types.QueryInitiativesByAssigneeRequest{Assignee: "worker1"})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, []uint64{1, 2, 3}, initiativeIDs(response.Initiatives))
	require.Equal(t, uint64(3), response.Pagination.Total)
}
