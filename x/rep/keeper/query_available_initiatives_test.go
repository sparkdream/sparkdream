package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// createInitiativeWithStatus creates a single initiative with the specified status
func createInitiativeWithStatus(k keeper.Keeper, ctx context.Context, id uint64, status types.InitiativeStatus) types.Initiative {
	amount := math.NewInt(int64(id) * 1000000)
	initiative := types.Initiative{
		Id:          id,
		ProjectId:   id,
		Title:       "Initiative " + strconv.FormatUint(id, 10),
		Description: "Description for initiative " + strconv.FormatUint(id, 10),
		Tier:        types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		Category:    types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		Budget:      &amount,
		Status:      status,
		CreatedAt:   1000,
	}
	_ = k.Initiative.Set(ctx, id, initiative)
	_ = k.InitiativeSeq.Set(ctx, id)
	return initiative
}

func initiativeIDs(list []types.Initiative) []uint64 {
	ids := make([]uint64, len(list))
	for i, ini := range list {
		ids[i] = ini.Id
	}
	return ids
}

func TestAvailableInitiatives(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*fixture)
		request *types.QueryAvailableInitiativesRequest
		wantIDs []uint64
		wantErr error
	}{
		{
			name: "ReturnsAllOpenInitiatives",
			setup: func(f *fixture) {
				createInitiativeWithStatus(f.keeper, f.ctx, 1, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeWithStatus(f.keeper, f.ctx, 2, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeWithStatus(f.keeper, f.ctx, 3, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeWithStatus(f.keeper, f.ctx, 4, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
			},
			request: &types.QueryAvailableInitiativesRequest{},
			wantIDs: []uint64{2, 3},
		},
		{
			name: "EmptyWhenNoOpenInitiatives",
			setup: func(f *fixture) {
				createInitiativeWithStatus(f.keeper, f.ctx, 1, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeWithStatus(f.keeper, f.ctx, 2, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
			},
			request: &types.QueryAvailableInitiativesRequest{},
			wantIDs: nil,
		},
		{
			name:    "EmptyWhenNoInitiativesExist",
			setup:   func(f *fixture) {},
			request: &types.QueryAvailableInitiativesRequest{},
			wantIDs: nil,
		},
		{
			name: "NewestFirstWithReverse",
			setup: func(f *fixture) {
				createInitiativeWithStatus(f.keeper, f.ctx, 1, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeWithStatus(f.keeper, f.ctx, 2, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeWithStatus(f.keeper, f.ctx, 3, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
			},
			request: &types.QueryAvailableInitiativesRequest{
				Pagination: &query.PageRequest{Reverse: true},
			},
			wantIDs: []uint64{3, 2, 1},
		},
		{
			name: "SortByBudgetDescending",
			setup: func(f *fixture) {
				// Budget scales with id, so budget-descending is id-descending.
				createInitiativeWithStatus(f.keeper, f.ctx, 1, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeWithStatus(f.keeper, f.ctx, 2, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeWithStatus(f.keeper, f.ctx, 3, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
			},
			request: &types.QueryAvailableInitiativesRequest{
				SortBy:     "budget",
				Pagination: &query.PageRequest{Reverse: true},
			},
			wantIDs: []uint64{3, 2, 1},
		},
		{
			name:    "UnknownSortKeyRejected",
			setup:   func(f *fixture) {},
			request: &types.QueryAvailableInitiativesRequest{SortBy: "bogus"},
			wantErr: status.Error(codes.InvalidArgument, `unknown sort_by "bogus" (want id, title, status, budget, tier or conviction)`),
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

			response, err := qs.AvailableInitiatives(f.ctx, tc.request)

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

func TestAvailableInitiatives_OffsetPagination(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	for id := uint64(1); id <= 5; id++ {
		createInitiativeWithStatus(f.keeper, f.ctx, id, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
	}

	first, err := qs.AvailableInitiatives(f.ctx, &types.QueryAvailableInitiativesRequest{
		Pagination: &query.PageRequest{Limit: 2, Reverse: true},
	})
	require.NoError(t, err)
	require.Equal(t, []uint64{5, 4}, initiativeIDs(first.Initiatives))
	require.Equal(t, uint64(5), first.Pagination.Total)
	require.NotEmpty(t, first.Pagination.NextKey)

	// Echoing next_key back continues from the same sorted position.
	second, err := qs.AvailableInitiatives(f.ctx, &types.QueryAvailableInitiativesRequest{
		Pagination: &query.PageRequest{Limit: 2, Reverse: true, Key: first.Pagination.NextKey},
	})
	require.NoError(t, err)
	require.Equal(t, []uint64{3, 2}, initiativeIDs(second.Initiatives))
	require.NotEmpty(t, second.Pagination.NextKey)

	third, err := qs.AvailableInitiatives(f.ctx, &types.QueryAvailableInitiativesRequest{
		Pagination: &query.PageRequest{Limit: 2, Reverse: true, Key: second.Pagination.NextKey},
	})
	require.NoError(t, err)
	require.Equal(t, []uint64{1}, initiativeIDs(third.Initiatives))
	require.Empty(t, third.Pagination.NextKey)
}
