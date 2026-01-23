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

func createInterimWithType(k keeper.Keeper, ctx context.Context, id uint64, interimType types.InterimType, status types.InterimStatus) types.Interim {
	interim := types.Interim{
		Id:            id,
		ReferenceType: "initiative",
		ReferenceId:   id,
		Type:          interimType,
		Assignees:     []string{"assignee1", "assignee2"},
		Status:        status,
		Deadline:      5000,
		CreatedAt:     1000,
		CompletedAt:   0,
	}
	_ = k.Interim.Set(ctx, id, interim)
	_ = k.InterimSeq.Set(ctx, id)
	return interim
}

func TestInterimsByType(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*fixture)
		interimType  uint64
		wantID       uint64
		wantStatus   uint64
		wantDeadline int64
		wantErr      error
	}{
		{
			name: "ReturnsFirstInterimForType",
			setup: func(f *fixture) {
				createInterimWithType(f.keeper, f.ctx, 1, types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, types.InterimStatus_INTERIM_STATUS_PENDING)
				createInterimWithType(f.keeper, f.ctx, 2, types.InterimType_INTERIM_TYPE_BUDGET_REVIEW, types.InterimStatus_INTERIM_STATUS_PENDING)
				createInterimWithType(f.keeper, f.ctx, 3, types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, types.InterimStatus_INTERIM_STATUS_COMPLETED)
			},
			interimType:  uint64(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL),
			wantID:       1,
			wantStatus:   uint64(types.InterimStatus_INTERIM_STATUS_PENDING),
			wantDeadline: 5000,
		},
		{
			name: "EmptyResponseWhenNoInterimsForType",
			setup: func(f *fixture) {
				createInterimWithType(f.keeper, f.ctx, 1, types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, types.InterimStatus_INTERIM_STATUS_PENDING)
				createInterimWithType(f.keeper, f.ctx, 2, types.InterimType_INTERIM_TYPE_BUDGET_REVIEW, types.InterimStatus_INTERIM_STATUS_PENDING)
			},
			interimType: uint64(types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY),
			wantErr:     nil,
		},
		{
			name:        "EmptyResponseWhenNoInterimsExist",
			setup:       func(f *fixture) {},
			interimType: uint64(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL),
			wantErr:     nil,
		},
		{
			name: "ReturnsInterimWithApprovedStatus",
			setup: func(f *fixture) {
				createInterimWithType(f.keeper, f.ctx, 1, types.InterimType_INTERIM_TYPE_BUDGET_REVIEW, types.InterimStatus_INTERIM_STATUS_COMPLETED)
				createInterimWithType(f.keeper, f.ctx, 2, types.InterimType_INTERIM_TYPE_BUDGET_REVIEW, types.InterimStatus_INTERIM_STATUS_PENDING)
			},
			interimType:  uint64(types.InterimType_INTERIM_TYPE_BUDGET_REVIEW),
			wantID:       1,
			wantStatus:   uint64(types.InterimStatus_INTERIM_STATUS_COMPLETED),
			wantDeadline: 5000,
		},
		{
			name:        "InvalidRequestNil",
			setup:       func(f *fixture) {},
			interimType: 0,
			wantErr:     status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			var req *types.QueryInterimsByTypeRequest
			if tc.wantErr == nil {
				req = &types.QueryInterimsByTypeRequest{InterimType: tc.interimType}
			}

			response, err := qs.InterimsByType(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantID > 0 {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantID, response.InterimId)
				require.Equal(t, tc.wantStatus, response.Status)
				require.Equal(t, tc.wantDeadline, response.Deadline)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, uint64(0), response.InterimId)
				require.Equal(t, uint64(0), response.Status)
				require.Equal(t, int64(0), response.Deadline)
			}
		})
	}
}

func TestInterimsByType_MultipleInterims(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple interims of the same type
	createInterimWithType(f.keeper, f.ctx, 1, types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, types.InterimStatus_INTERIM_STATUS_PENDING)
	createInterimWithType(f.keeper, f.ctx, 2, types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, types.InterimStatus_INTERIM_STATUS_COMPLETED)
	createInterimWithType(f.keeper, f.ctx, 3, types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL, types.InterimStatus_INTERIM_STATUS_COMPLETED)

	// Query should return first interim (id 1)
	response, err := qs.InterimsByType(f.ctx, &types.QueryInterimsByTypeRequest{
		InterimType: uint64(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL),
	})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, uint64(1), response.InterimId)
	require.Equal(t, uint64(types.InterimStatus_INTERIM_STATUS_PENDING), response.Status)
	require.Equal(t, int64(5000), response.Deadline)
}
