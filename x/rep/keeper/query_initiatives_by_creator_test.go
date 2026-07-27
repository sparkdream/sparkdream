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

func createInitiativeForCreator(k keeper.Keeper, ctx context.Context, id uint64, creator string, status types.InitiativeStatus) types.Initiative {
	amount := math.NewInt(1000000)
	initiative := types.Initiative{
		Id:          id,
		ProjectId:   id,
		Title:       "Initiative " + strconv.FormatUint(id, 10),
		Description: "Description for initiative " + strconv.FormatUint(id, 10),
		Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		Category:    types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		Budget:      &amount,
		Creator:     creator,
		Status:      status,
		CreatedAt:   1000,
	}
	_ = k.Initiative.Set(ctx, id, initiative)
	_ = k.InitiativeSeq.Set(ctx, id)
	return initiative
}

func TestInitiativesByCreator(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*fixture)
		request *types.QueryInitiativesByCreatorRequest
		wantIDs []uint64
		wantErr error
	}{
		{
			name: "ReturnsEveryInitiativeForCreator",
			setup: func(f *fixture) {
				createInitiativeForCreator(f.keeper, f.ctx, 1, "creator1", types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeForCreator(f.keeper, f.ctx, 2, "creator2", types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeForCreator(f.keeper, f.ctx, 3, "creator1", types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
			},
			request: &types.QueryInitiativesByCreatorRequest{Creator: "creator1"},
			wantIDs: []uint64{1, 3},
		},
		{
			name: "CreatorIsIndependentOfAssignee",
			setup: func(f *fixture) {
				ini := createInitiativeForCreator(f.keeper, f.ctx, 1, "creator1", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				ini.Assignee = "worker1"
				_ = f.keeper.Initiative.Set(f.ctx, ini.Id, ini)
			},
			request: &types.QueryInitiativesByCreatorRequest{Creator: "worker1"},
			wantIDs: nil,
		},
		{
			name: "EmptyWhenNoInitiativesForCreator",
			setup: func(f *fixture) {
				createInitiativeForCreator(f.keeper, f.ctx, 1, "creator1", types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
			},
			request: &types.QueryInitiativesByCreatorRequest{Creator: "nonexistent"},
			wantIDs: nil,
		},
		{
			name:    "EmptyWhenNoInitiativesExist",
			setup:   func(f *fixture) {},
			request: &types.QueryInitiativesByCreatorRequest{Creator: "creator1"},
			wantIDs: nil,
		},
		{
			name: "SortByStatus",
			setup: func(f *fixture) {
				createInitiativeForCreator(f.keeper, f.ctx, 1, "creatorX", types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
				createInitiativeForCreator(f.keeper, f.ctx, 2, "creatorX", types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
			},
			request: &types.QueryInitiativesByCreatorRequest{Creator: "creatorX", SortBy: "status"},
			wantIDs: []uint64{2, 1},
		},
		{
			name: "UnknownSortKeyRejected",
			setup: func(f *fixture) {
				createInitiativeForCreator(f.keeper, f.ctx, 1, "creatorX", types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
			},
			request: &types.QueryInitiativesByCreatorRequest{Creator: "creatorX", SortBy: "bogus"},
			wantErr: status.Error(codes.InvalidArgument, `unknown sort_by "bogus" (want id, title, status, budget, tier or conviction)`),
		},
		{
			name:    "EmptyCreatorRejected",
			setup:   func(f *fixture) {},
			request: &types.QueryInitiativesByCreatorRequest{Creator: ""},
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

			response, err := qs.InitiativesByCreator(f.ctx, tc.request)

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
