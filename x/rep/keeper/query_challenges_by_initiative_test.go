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

func createChallengeForInitiative(k keeper.Keeper, ctx context.Context, id uint64, initiativeID uint64, status types.ChallengeStatus) types.Challenge {
	amount := math.NewInt(1000000)
	challenge := types.Challenge{
		Id:            id,
		InitiativeId:  initiativeID,
		Challenger:    "challenger" + strconv.FormatUint(id, 10),
		Reason:        "Test challenge",
		StakedDream:   &amount,
		IsAnonymous:   true,
		PayoutAddress: "sprkdr" + strconv.FormatUint(id, 10) + "address",
		Status:        status,
		CreatedAt:     1000,
		ResolvedAt:    0,
	}
	_ = k.Challenge.Set(ctx, id, challenge)
	_ = k.ChallengeSeq.Set(ctx, id)
	return challenge
}

func TestChallengesByInitiative(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(*fixture)
		initiativeID    uint64
		wantChallengeID uint64
		wantStatus      uint64
		wantErr         error
	}{
		{
			name: "ReturnsFirstChallengeForInitiative",
			setup: func(f *fixture) {
				createChallengeForInitiative(f.keeper, f.ctx, 1, 1, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE)
				createChallengeForInitiative(f.keeper, f.ctx, 2, 2, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE)
				createChallengeForInitiative(f.keeper, f.ctx, 3, 1, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD)
			},
			initiativeID:    1,
			wantChallengeID: 1,
			wantStatus:      uint64(types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE),
		},
		{
			name: "EmptyResponseWhenNoChallengesForInitiative",
			setup: func(f *fixture) {
				createChallengeForInitiative(f.keeper, f.ctx, 1, 1, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE)
				createChallengeForInitiative(f.keeper, f.ctx, 2, 2, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE)
			},
			initiativeID: 3,
			wantErr:      nil,
		},
		{
			name:         "EmptyResponseWhenNoChallengesExist",
			setup:        func(f *fixture) {},
			initiativeID: 1,
			wantErr:      nil,
		},
		{
			name: "ReturnsChallengeWithUpheldStatus",
			setup: func(f *fixture) {
				createChallengeForInitiative(f.keeper, f.ctx, 1, 5, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD)
				createChallengeForInitiative(f.keeper, f.ctx, 2, 5, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE)
			},
			initiativeID:    5,
			wantChallengeID: 1,
			wantStatus:      uint64(types.ChallengeStatus_CHALLENGE_STATUS_UPHELD),
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

			var req *types.QueryChallengesByInitiativeRequest
			if tc.initiativeID > 0 || tc.wantErr == nil {
				req = &types.QueryChallengesByInitiativeRequest{InitiativeId: tc.initiativeID}
			}

			response, err := qs.ChallengesByInitiative(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantChallengeID > 0 {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantChallengeID, response.ChallengeId)
				require.Equal(t, tc.wantStatus, response.Status)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, uint64(0), response.ChallengeId)
				require.Equal(t, uint64(0), response.Status)
			}
		})
	}
}

func TestChallengesByInitiative_MultipleChallenges(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple challenges for the same initiative
	createChallengeForInitiative(f.keeper, f.ctx, 1, 10, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE)
	createChallengeForInitiative(f.keeper, f.ctx, 2, 10, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW)
	createChallengeForInitiative(f.keeper, f.ctx, 3, 10, types.ChallengeStatus_CHALLENGE_STATUS_REJECTED)

	// Query should return the first challenge (id 1)
	response, err := qs.ChallengesByInitiative(f.ctx, &types.QueryChallengesByInitiativeRequest{InitiativeId: 10})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, uint64(1), response.ChallengeId)
	require.Equal(t, uint64(types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE), response.Status)
}
