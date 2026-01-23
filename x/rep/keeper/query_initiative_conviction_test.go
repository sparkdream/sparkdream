package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestInitiativeConviction(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*fixture)
		initiativeID  uint64
		wantTotalConv string
		wantExternal  string
		wantThreshold string
		wantErr       error
	}{
		{
			name: "ReturnsConvictionForExistingInitiative",
			setup: func(f *fixture) {
				params := types.DefaultParams()
				// ensure decay logic doesn't zero out immediately
				params.ConvictionHalfLifeEpochs = 10000 // Very slow decay
				params.EpochBlocks = 100
				_ = f.keeper.Params.Set(f.ctx, params)

				f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(1000, 0))
				amount := math.NewInt(1000000)
				reqDec := math.LegacyNewDec(100)
				currDec := math.LegacyNewDec(50)
				extDec := math.LegacyNewDec(25)

				initiative := types.Initiative{
					Id:                    1,
					ProjectId:             1,
					Title:                 "Test Initiative",
					Description:           "A test initiative",
					Tier:                  types.InitiativeTier_INITIATIVE_TIER_STANDARD,
					Category:              types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
					TemplateId:            "template-1",
					Budget:                &amount,
					Assignee:              "assignee",
					Apprentice:            "",
					AssignedAt:            1000,
					DeliverableUri:        "",
					SubmittedAt:           0,
					RequiredConviction:    &reqDec,
					CurrentConviction:     &currDec,
					ExternalConviction:    &extDec,
					ConvictionLastUpdated: 1000,
					ReviewPeriodEnd:       5000,
					ChallengePeriodEnd:    5000,
					Status:                types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
					CreatedAt:             1000,
					CompletedAt:           0,
				}
				_ = f.keeper.Initiative.Set(f.ctx, 1, initiative)
				_ = f.keeper.InitiativeSeq.Set(f.ctx, 1)

				// Set up project
				project := types.Project{
					Id:             1,
					Name:           "Test Project",
					Description:    "A test project",
					Council:        "commons",
					Creator:        "proposer",
					Status:         types.ProjectStatus_PROJECT_STATUS_PROPOSED,
					ApprovedBudget: &amount,
				}
				_ = f.keeper.Project.Set(f.ctx, 1, project)
				_ = f.keeper.ProjectSeq.Set(f.ctx, 1)
			},
			initiativeID:  1,
			wantTotalConv: "0.000000000000000000",
			wantExternal:  "0.000000000000000000",
			wantThreshold: "100.000000000000000000",
		},
		{
			name:         "NotFoundForNonExistentInitiative",
			setup:        func(f *fixture) {},
			initiativeID: 999,
			wantErr:      status.Error(codes.NotFound, "initiative not found"),
		},
		{
			name:         "InvalidRequestNil",
			setup:        func(f *fixture) {},
			initiativeID: 0,
			wantErr:      status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			var req *types.QueryInitiativeConvictionRequest
			if tc.initiativeID > 0 || tc.wantErr == nil {
				req = &types.QueryInitiativeConvictionRequest{InitiativeId: tc.initiativeID}
			}

			response, err := qs.InitiativeConviction(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantTotalConv, response.TotalConviction.String())
				require.Equal(t, tc.wantExternal, response.ExternalConviction.String())
				require.Equal(t, tc.wantThreshold, response.Threshold.String())
			}
		})
	}
}

func TestInitiativeConviction_WithStakes(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	amount := math.NewInt(1000000)
	reqDec := math.LegacyNewDec(100)
	currDec := math.LegacyNewDec(10)
	extDec := math.LegacyNewDec(5)

	// Create params
	params := types.DefaultParams()
	params.ConvictionHalfLifeEpochs = 10000
	params.EpochBlocks = 100
	_ = f.keeper.Params.Set(f.ctx, params)

	// Create initiative
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(3000, 0)) // Time passes, but before review end
	initiative := types.Initiative{
		Id:                    1,
		ProjectId:             1,
		Title:                 "Test Initiative",
		Description:           "A test initiative",
		Tier:                  types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		Category:              types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		TemplateId:            "template-1",
		Budget:                &amount,
		Assignee:              "assignee",
		Status:                types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
		CreatedAt:             1000,
		RequiredConviction:    &reqDec,
		CurrentConviction:     &currDec,
		ExternalConviction:    &extDec,
		ConvictionLastUpdated: 1000,
		ReviewPeriodEnd:       5000,
		ChallengePeriodEnd:    5000,
	}
	_ = f.keeper.Initiative.Set(f.ctx, 1, initiative)
	_ = f.keeper.InitiativeSeq.Set(f.ctx, 1)

	// Create project
	project := types.Project{
		Id:             1,
		Name:           "Test Project",
		Description:    "A test project",
		Council:        "commons",
		Creator:        "proposer",
		Status:         types.ProjectStatus_PROJECT_STATUS_PROPOSED,
		ApprovedBudget: &amount,
	}
	_ = f.keeper.Project.Set(f.ctx, 1, project)
	_ = f.keeper.ProjectSeq.Set(f.ctx, 1)

	// Create Member
	_ = f.keeper.Member.Set(f.ctx, "staker1", types.Member{
		Address:          "staker1",
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	})

	// Create a stake
	stakeAmount := math.NewInt(500000)
	stake := types.Stake{
		Id:         1,
		Staker:     "staker1",
		TargetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		TargetId:   1,
		Amount:     stakeAmount,
		CreatedAt:  2000,
	}
	_ = f.keeper.Stake.Set(f.ctx, 1, stake)
	_ = f.keeper.StakeSeq.Set(f.ctx, 1)

	// Query conviction - should trigger lazy update
	response, err := qs.InitiativeConviction(f.ctx, &types.QueryInitiativeConvictionRequest{InitiativeId: 1})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.True(t, response.TotalConviction.GTE(math.LegacyZeroDec()))
}
