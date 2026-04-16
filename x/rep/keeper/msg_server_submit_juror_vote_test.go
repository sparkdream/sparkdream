package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerSubmitJurorVote(t *testing.T) {
	t.Run("invalid juror address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SubmitJurorVote(f.ctx, &types.MsgSubmitJurorVote{
			Juror:         "invalid-address",
			JuryReviewId:  1,
			CriteriaVotes: []*types.CriteriaVote{},
			Verdict:       types.Verdict_VERDICT_REJECT_CHALLENGE,
			Confidence:    keeper.PtrDec(math.LegacyZeroDec()),
			Reasoning:     "Reason",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid juror address")
	})

	t.Run("missing confidence", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		juror := sdk.AccAddress([]byte("juror"))
		jurorStr, err := f.addressCodec.BytesToString(juror)
		require.NoError(t, err)

		_, err = ms.SubmitJurorVote(f.ctx, &types.MsgSubmitJurorVote{
			Juror:         jurorStr,
			JuryReviewId:  1,
			CriteriaVotes: []*types.CriteriaVote{},
			Verdict:       1,
			Confidence:    nil,
			Reasoning:     "Reason",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "confidence is required")
	})

	t.Run("successful vote", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup params
		params := types.DefaultParams()
		params.JurySize = 3
		_ = k.Params.Set(ctx, params)

		// Setup: create jury review
		juror1 := sdk.AccAddress([]byte("juror1"))
		juror2 := sdk.AccAddress([]byte("juror2"))
		juror3 := sdk.AccAddress([]byte("juror3"))

		k.Member.Set(ctx, juror1.String(), types.Member{
			Address:          juror1.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		k.Member.Set(ctx, juror2.String(), types.Member{
			Address:          juror2.String(),
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})
		k.Member.Set(ctx, juror3.String(), types.Member{
			Address:          juror3.String(),
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		// Create project, initiative, challenge
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
		challenger := sdk.AccAddress([]byte("challenger"))
		challenge := types.Challenge{
			Id:           1,
			InitiativeId: initID,
			Challenger:   challenger.String(),
			Status:       types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		}
		_ = k.Challenge.Set(ctx, 1, challenge)
		challengeID := uint64(1)

		err := k.CreateJuryReview(ctx, challengeID, "response", []string{}) // wantErr: "collections: not found"
		require.NoError(t, err)

		jurorStr, err := f.addressCodec.BytesToString(juror1)
		require.NoError(t, err)

		var juryReviewID uint64
		err = k.JuryReview.Walk(ctx, nil, func(id uint64, jr types.JuryReview) (bool, error) {
			juryReviewID = id
			jr.Jurors = []string{jurorStr}
			_ = k.JuryReview.Set(ctx, id, jr)
			return true, nil
		})
		require.NoError(t, err)

		dec, err := math.LegacyNewDecFromStr("0.9")
		require.NoError(t, err)

		// Submit vote
		_, err = ms.SubmitJurorVote(ctx, &types.MsgSubmitJurorVote{
			Juror:        jurorStr,
			JuryReviewId: juryReviewID,
			CriteriaVotes: []*types.CriteriaVote{{
				CriteriaId: "quality",
				Score:      5,
				Notes:      "Excellent work",
			}},
			Verdict:    types.Verdict_VERDICT_REJECT_CHALLENGE,
			Confidence: keeper.PtrDec(dec),
			Reasoning:  "The work meets all requirements",
		})
		require.NoError(t, err)

		// Verify vote exists
		juryReview, err := k.GetJuryReview(ctx, juryReviewID)
		require.NoError(t, err)
		require.Len(t, juryReview.Votes, 1)
	})
}
