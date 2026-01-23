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

func createInterimWithReference(k keeper.Keeper, ctx context.Context, id uint64, refType string, refID uint64, status types.InterimStatus) types.Interim {
	interim := types.Interim{
		Id:            id,
		ReferenceType: refType,
		ReferenceId:   refID,
		Type:          types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL,
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

func TestInterimsByReference(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*fixture)
		referenceType string
		referenceID   uint64
		wantID        uint64
		wantType      uint64
		wantStatus    uint64
		wantErr       error
	}{
		{
			name: "ReturnsFirstInterimForReference",
			setup: func(f *fixture) {
				createInterimWithReference(f.keeper, f.ctx, 1, "initiative", 10, types.InterimStatus_INTERIM_STATUS_PENDING)
				createInterimWithReference(f.keeper, f.ctx, 2, "project", 20, types.InterimStatus_INTERIM_STATUS_PENDING)
				createInterimWithReference(f.keeper, f.ctx, 3, "initiative", 10, types.InterimStatus_INTERIM_STATUS_COMPLETED)
			},
			referenceType: "initiative",
			referenceID:   10,
			wantID:        1,
			wantType:      uint64(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL),
			wantStatus:    uint64(types.InterimStatus_INTERIM_STATUS_PENDING),
		},
		{
			name: "EmptyResponseWhenNoInterimsForReference",
			setup: func(f *fixture) {
				createInterimWithReference(f.keeper, f.ctx, 1, "initiative", 1, types.InterimStatus_INTERIM_STATUS_PENDING)
				createInterimWithReference(f.keeper, f.ctx, 2, "project", 2, types.InterimStatus_INTERIM_STATUS_PENDING)
			},
			referenceType: "initiative",
			referenceID:   99,
			wantErr:       nil,
		},
		{
			name:          "EmptyResponseWhenNoInterimsExist",
			setup:         func(f *fixture) {},
			referenceType: "initiative",
			referenceID:   1,
			wantErr:       nil,
		},
		{
			name: "ReturnsInterimForProjectReference",
			setup: func(f *fixture) {
				createInterimWithReference(f.keeper, f.ctx, 1, "project", 100, types.InterimStatus_INTERIM_STATUS_COMPLETED)
				createInterimWithReference(f.keeper, f.ctx, 2, "project", 100, types.InterimStatus_INTERIM_STATUS_COMPLETED)
			},
			referenceType: "project",
			referenceID:   100,
			wantID:        1,
			wantType:      uint64(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL),
			wantStatus:    uint64(types.InterimStatus_INTERIM_STATUS_COMPLETED),
		},
		{
			name:          "InvalidRequestNil",
			setup:         func(f *fixture) {},
			referenceType: "",
			referenceID:   0,
			wantErr:       status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			var req *types.QueryInterimsByReferenceRequest
			if tc.wantErr == nil {
				req = &types.QueryInterimsByReferenceRequest{
					ReferenceType: tc.referenceType,
					ReferenceId:   tc.referenceID,
				}
			}

			response, err := qs.InterimsByReference(f.ctx, req)

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

func TestInterimsByReference_MultipleInterims(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple interims for the same reference
	createInterimWithReference(f.keeper, f.ctx, 1, "initiative", 50, types.InterimStatus_INTERIM_STATUS_PENDING)
	createInterimWithReference(f.keeper, f.ctx, 2, "initiative", 50, types.InterimStatus_INTERIM_STATUS_COMPLETED)
	createInterimWithReference(f.keeper, f.ctx, 3, "initiative", 50, types.InterimStatus_INTERIM_STATUS_COMPLETED)

	// Query should return first interim (id 1)
	response, err := qs.InterimsByReference(f.ctx, &types.QueryInterimsByReferenceRequest{
		ReferenceType: "initiative",
		ReferenceId:   50,
	})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, uint64(1), response.InterimId)
	require.Equal(t, uint64(types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL), response.InterimType)
	require.Equal(t, uint64(types.InterimStatus_INTERIM_STATUS_PENDING), response.Status)
}
