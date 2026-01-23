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

// createInitiativeWithStatus creates a single initiative with the specified status
func createInitiativeWithStatus(k keeper.Keeper, ctx context.Context, id uint64, status types.InitiativeStatus) types.Initiative {
	amount := math.NewInt(1000000)
	initiative := types.Initiative{
		Id:         id,
		ProjectId:  id,
		Title:      "Initiative " + strconv.FormatUint(id, 10),
		Description: "Description for initiative " + strconv.FormatUint(id, 10),
		Tier:       types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		Category:   types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		TemplateId: "template-" + strconv.FormatUint(id, 10),
		Budget:     &amount,
		Status:     status,
		CreatedAt:  1000,
	}
	_ = k.Initiative.Set(ctx, id, initiative)
	_ = k.InitiativeSeq.Set(ctx, id)
	return initiative
}

func TestAvailableInitiatives(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*fixture)
		request  *types.QueryAvailableInitiativesRequest
		wantId   uint64
		wantTitle string
		wantTier uint64
		wantErr  error
	}{
		{
			name: "ReturnsFirstAvailableInitiative",
			setup: func(f *fixture) {
				// Create multiple initiatives with different statuses
				createInitiativeWithStatus(f.keeper, f.ctx, 1, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeWithStatus(f.keeper, f.ctx, 2, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeWithStatus(f.keeper, f.ctx, 3, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
				createInitiativeWithStatus(f.keeper, f.ctx, 4, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
			},
			request:  &types.QueryAvailableInitiativesRequest{},
			wantId:   2,
			wantTitle: "Initiative 2",
			wantTier: uint64(types.InitiativeTier_INITIATIVE_TIER_STANDARD),
		},
		{
			name: "EmptyResponseWhenNoOpenInitiatives",
			setup: func(f *fixture) {
				// Only create non-OPEN initiatives
				createInitiativeWithStatus(f.keeper, f.ctx, 1, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
				createInitiativeWithStatus(f.keeper, f.ctx, 2, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
			},
			request:  &types.QueryAvailableInitiativesRequest{},
			wantErr: nil,
		},
		{
			name:    "EmptyResponseWhenNoInitiativesExist",
			setup:   func(f *fixture) {},
			request: &types.QueryAvailableInitiativesRequest{},
			wantErr: nil,
		},
		{
			name: "ReturnsSingleOpenInitiative",
			setup: func(f *fixture) {
				amount := math.NewInt(5000000)
				initiative := types.Initiative{
					Id:         1,
					ProjectId:  1,
					Title:      "Test Initiative",
					Description: "A test initiative",
					Tier:       types.InitiativeTier_INITIATIVE_TIER_EXPERT,
					Category:   types.InitiativeCategory_INITIATIVE_CATEGORY_DESIGN,
					TemplateId: "template-1",
					Budget:     &amount,
					Status:     types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
					CreatedAt:  1000,
				}
				_ = f.keeper.Initiative.Set(f.ctx, 1, initiative)
				_ = f.keeper.InitiativeSeq.Set(f.ctx, 1)
			},
			request:   &types.QueryAvailableInitiativesRequest{},
			wantId:    1,
			wantTitle: "Test Initiative",
			wantTier:  uint64(types.InitiativeTier_INITIATIVE_TIER_EXPERT),
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

			// Setup test data
			if tc.setup != nil {
				tc.setup(f)
			}

			// Execute query
			response, err := qs.AvailableInitiatives(f.ctx, tc.request)

			// Check results
			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantId > 0 {
				// Should return an initiative
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantId, response.InitiativeId)
				require.Equal(t, tc.wantTitle, response.Title)
				require.Equal(t, tc.wantTier, response.Tier)
			} else {
				// Should return empty response
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, uint64(0), response.InitiativeId)
				require.Equal(t, "", response.Title)
				require.Equal(t, uint64(0), response.Tier)
			}
		})
	}
}

func TestAvailableInitiatives_MultipleOpenInitiatives(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple OPEN initiatives in a specific order
	initiatives := []struct {
		id    uint64
		title string
		tier  types.InitiativeTier
	}{
		{1, "First Open Initiative", types.InitiativeTier_INITIATIVE_TIER_STANDARD},
		{2, "Second Open Initiative", types.InitiativeTier_INITIATIVE_TIER_EXPERT},
		{3, "Third Open Initiative", types.InitiativeTier_INITIATIVE_TIER_EPIC},
	}

	for _, init := range initiatives {
		amount := math.NewInt(int64(init.id * 1000000))
		initiative := types.Initiative{
			Id:         init.id,
			ProjectId:  init.id,
			Title:      init.title,
			Description: "Description for " + init.title,
			Tier:       init.tier,
			Category:   types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
			TemplateId: "template-" + strconv.FormatUint(init.id, 10),
			Budget:     &amount,
			Status:     types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
			CreatedAt:  int64(init.id * 1000),
		}
		_ = f.keeper.Initiative.Set(f.ctx, init.id, initiative)
		_ = f.keeper.InitiativeSeq.Set(f.ctx, init.id)
	}

	// Query should return the first available initiative (id 1)
	response, err := qs.AvailableInitiatives(f.ctx, &types.QueryAvailableInitiativesRequest{})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, uint64(1), response.InitiativeId)
	require.Equal(t, "First Open Initiative", response.Title)
	require.Equal(t, uint64(types.InitiativeTier_INITIATIVE_TIER_STANDARD), response.Tier)
}
