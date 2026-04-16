package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerSubmitExpertTestimony(t *testing.T) {
	t.Run("invalid expert address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SubmitExpertTestimony(f.ctx, &types.MsgSubmitExpertTestimony{
			Expert:       "invalid-address",
			JuryReviewId: 1,
			Opinion:      "Opinion",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid expert address")
	})

	t.Run("non-existent jury review", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		expert := sdk.AccAddress([]byte("expert"))
		expertStr, err := f.addressCodec.BytesToString(expert)
		require.NoError(t, err)

		_, err = ms.SubmitExpertTestimony(f.ctx, &types.MsgSubmitExpertTestimony{
			Expert:       expertStr,
			JuryReviewId: 99999,
			Opinion:      "Opinion",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("successful testimony", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create challenge and jury review
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))

		// Assign and submit work to make initiative eligible for challenge
		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		k.Member.Set(ctx, assigneeStr, types.Member{
			Address:          assigneeStr,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		err = k.AssignInitiativeToMember(ctx, initID, assignee)
		require.NoError(t, err)
		err = k.SubmitInitiativeWork(ctx, initID, assignee, "uri")
		require.NoError(t, err)

		challenger := sdk.AccAddress([]byte("challenger"))
		k.Member.Set(ctx, challenger.String(), types.Member{
			Address:          challenger.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(1000)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		// Create challenge manually
		challengeID := uint64(100)
		challenge := types.Challenge{
			Id:           challengeID,
			InitiativeId: initID,
			Challenger:   challenger.String(),
			Status:       types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
			StakedDream:  keeper.PtrInt(math.NewInt(100)),
		}
		err = k.Challenge.Set(ctx, challengeID, challenge)
		require.NoError(t, err)

		// Update initiative status
		initiative, _ := k.GetInitiative(ctx, initID)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED
		k.Initiative.Set(ctx, initID, initiative)

		// Set jury size to 1 for easier testing
		params, _ := k.Params.Get(ctx)
		params.JurySize = 1
		k.Params.Set(ctx, params)

		// Add a juror
		juror := sdk.AccAddress([]byte("juror"))
		k.Member.Set(ctx, juror.String(), types.Member{
			Address:          juror.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(1000)),
			Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_TRUSTED,
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		// Manually create JuryReview to bypass possible issue in CreateJuryReview/Sequence
		juryReviewID := uint64(100)
		juryReview := types.JuryReview{
			Id:              juryReviewID,
			ChallengeId:     challengeID,
			InitiativeId:    initID,
			Jurors:          []string{juror.String()},
			RequiredVotes:   1,
			ExpertWitnesses: []string{},
			Testimonies:     []*types.ExpertTestimony{},
			Deadline:        sdk.UnwrapSDKContext(ctx).BlockHeight() + 100,
			Verdict:         types.Verdict_VERDICT_PENDING,
			Votes:           []*types.JurorVote{},
		}
		err = k.JuryReview.Set(ctx, juryReviewID, juryReview)
		require.NoError(t, err)

		expert := sdk.AccAddress([]byte("expert"))
		expertStr, err := f.addressCodec.BytesToString(expert)
		require.NoError(t, err)

		// Add expert to witness list
		juryReview.ExpertWitnesses = append(juryReview.ExpertWitnesses, expertStr)
		k.JuryReview.Set(ctx, juryReviewID, juryReview)

		// Submit testimony
		_, err = ms.SubmitExpertTestimony(ctx, &types.MsgSubmitExpertTestimony{
			Expert:       expertStr,
			JuryReviewId: juryReviewID,
			Opinion:      "Opinion",
			Reasoning:    "Reasoning",
		})
		require.NoError(t, err)
	})
}
