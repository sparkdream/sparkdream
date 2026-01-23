package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerRespondToChallenge(t *testing.T) {
	t.Run("invalid assignee address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.RespondToChallenge(f.ctx, &types.MsgRespondToChallenge{
			Assignee:    "invalid-address",
			ChallengeId: 1,
			Response:    "Response",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid assignee address")
	})

	t.Run("non-existent challenge", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		_, err = ms.RespondToChallenge(f.ctx, &types.MsgRespondToChallenge{
			Assignee:    assigneeStr,
			ChallengeId: 99999,
			Response:    "Response",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("successful response", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create challenge
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
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

		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "uri")

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

		// Add a juror to ensure jury selection succeeds
		juror := sdk.AccAddress([]byte("juror"))
		k.Member.Set(ctx, juror.String(), types.Member{
			Address:          juror.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(1000)),
			Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_TRUSTED,
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		// Respond to challenge (expect routing to jury)
		_, err = ms.RespondToChallenge(ctx, &types.MsgRespondToChallenge{
			Assignee:    assigneeStr,
			ChallengeId: challengeID,
			Response:    "Response",
			Evidence:    []string{"evidence_uri"},
		})
		require.NoError(t, err)

		// Check if challenge status is updated to IN_JURY_REVIEW
		updatedChallenge, err := k.GetChallenge(ctx, challengeID)
		require.NoError(t, err)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, updatedChallenge.Status)
	})
}
