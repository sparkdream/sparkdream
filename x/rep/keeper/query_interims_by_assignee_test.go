package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createInterimWithAssignees(k keeper.Keeper, ctx context.Context, id uint64, assignees []string, status types.InterimStatus) types.Interim {
	// Create interim
	interim := types.Interim{
		Id:            id,
		ReferenceType: "initiative",
		ReferenceId:   id,
		Type:          types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL,
		Assignees:     assignees,
		Status:        status,
		Deadline:      5000,
		CreatedAt:     1000,
		CompletedAt:   0,
	}
	_ = k.Interim.Set(ctx, id, interim)
	_ = k.InterimSeq.Set(ctx, id)
	return interim
}

func TestInterimsByAssignee(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*fixture)
		assignee   string
		wantID     uint64
		wantType   uint64
		wantStatus uint64
		wantErr    error
	}{
		{
			name: "ReturnsFirstInterimForAssignee",
			setup: func(f *fixture) {
				createInterimWithAssignees(f.keeper, f.ctx, 1, []string{"worker1", "worker2"}, types.InterimStatus_INTERIM_STATUS_PENDING)
				createInterimWithAssignees(f.keeper, f.ctx, 2, []string{"worker3"}, types.InterimStatus_INTERIM_STATUS_PENDING)
				createInterimWithAssignees(f.keeper, f.ctx, 3, []string{"worker1", "worker3"}, types.InterimStatus_INTERIM_STATUS_COMPLETED)
			},
			assignee:   "worker1",
			wantID:     1,
			wantType:   uint64(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL),
			wantStatus: uint64(types.InterimStatus_INTERIM_STATUS_PENDING),
		},
		{
			name: "EmptyResponseWhenNoInterimsForAssignee",
			setup: func(f *fixture) {
				createInterimWithAssignees(f.keeper, f.ctx, 1, []string{"worker1"}, types.InterimStatus_INTERIM_STATUS_PENDING)
				createInterimWithAssignees(f.keeper, f.ctx, 2, []string{"worker2"}, types.InterimStatus_INTERIM_STATUS_PENDING)
			},
			assignee: "nonexistent",
			wantErr:  nil,
		},
		{
			name:     "EmptyResponseWhenNoInterimsExist",
			setup:    func(f *fixture) {},
			assignee: "worker1",
			wantErr:  nil,
		},
		{
			name: "ReturnsInterimWithRejectedStatus",
			setup: func(f *fixture) {
				createInterimWithAssignees(f.keeper, f.ctx, 1, []string{"reviewerX"}, types.InterimStatus_INTERIM_STATUS_COMPLETED)
				createInterimWithAssignees(f.keeper, f.ctx, 2, []string{"reviewerX"}, types.InterimStatus_INTERIM_STATUS_COMPLETED)
			},
			assignee:   "reviewerX",
			wantID:     1,
			wantType:   uint64(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL),
			wantStatus: uint64(types.InterimStatus_INTERIM_STATUS_COMPLETED),
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

			var req *types.QueryInterimsByAssigneeRequest
			if tc.assignee != "" || tc.wantErr == nil {
				req = &types.QueryInterimsByAssigneeRequest{Assignee: tc.assignee}
			}

			response, err := qs.InterimsByAssignee(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantID > 0 {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantID, response.InterimId)
				require.Equal(t, tc.wantType, response.InterimType)
				require.Equal(t, tc.wantStatus, response.Status)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, uint64(0), response.InterimId)
				require.Equal(t, uint64(0), response.InterimType)
				require.Equal(t, uint64(0), response.Status)
			}
		})
	}
}

func TestInterimsByAssignee_MultipleInterims(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple interims for the same assignee
	createInterimWithAssignees(f.keeper, f.ctx, 1, []string{"committee1", "committee2"}, types.InterimStatus_INTERIM_STATUS_PENDING)
	createInterimWithAssignees(f.keeper, f.ctx, 2, []string{"committee1"}, types.InterimStatus_INTERIM_STATUS_COMPLETED)
	createInterimWithAssignees(f.keeper, f.ctx, 3, []string{"committee1", "committee3"}, types.InterimStatus_INTERIM_STATUS_COMPLETED)

	// Query should return first interim (id 1)
	response, err := qs.InterimsByAssignee(f.ctx, &types.QueryInterimsByAssigneeRequest{Assignee: "committee1"})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, uint64(1), response.InterimId)
	require.Equal(t, uint64(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL), response.InterimType)
	require.Equal(t, uint64(types.InterimStatus_INTERIM_STATUS_PENDING), response.Status)
}
