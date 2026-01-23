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

func createInvitationForInviter(k keeper.Keeper, ctx context.Context, id uint64, inviter string, status types.InvitationStatus) types.Invitation {
	amount := math.NewInt(1000000)
	invitation := types.Invitation{
		Id:                id,
		Inviter:           inviter,
		InviteeAddress:    "sprkdr" + strconv.FormatUint(id, 10) + "address",
		StakedDream:       &amount,
		AccountabilityEnd: 0,
		ReferralEnd:       0,
		Status:            status,
		CreatedAt:         1000,
	}
	_ = k.Invitation.Set(ctx, id, invitation)
	_ = k.InvitationSeq.Set(ctx, id)
	return invitation
}

func TestInvitationsByInviter(t *testing.T) {
	tests := []struct {
		name             string
		setup            func(*fixture)
		inviter          string
		wantInvitationID uint64
		wantInviteeAddr  string
		wantStatus       uint64
		wantErr          error
	}{
		{
			name: "ReturnsFirstInvitationForInviter",
			setup: func(f *fixture) {
				createInvitationForInviter(f.keeper, f.ctx, 1, "inviter1", types.InvitationStatus_INVITATION_STATUS_PENDING)
				createInvitationForInviter(f.keeper, f.ctx, 2, "inviter2", types.InvitationStatus_INVITATION_STATUS_PENDING)
				createInvitationForInviter(f.keeper, f.ctx, 3, "inviter1", types.InvitationStatus_INVITATION_STATUS_ACCEPTED)
			},
			inviter:          "inviter1",
			wantInvitationID: 1,
			wantInviteeAddr:  "sprkdr1address",
			wantStatus:       uint64(types.InvitationStatus_INVITATION_STATUS_PENDING),
		},
		{
			name: "EmptyResponseWhenNoInvitationsForInviter",
			setup: func(f *fixture) {
				createInvitationForInviter(f.keeper, f.ctx, 1, "inviter1", types.InvitationStatus_INVITATION_STATUS_PENDING)
				createInvitationForInviter(f.keeper, f.ctx, 2, "inviter2", types.InvitationStatus_INVITATION_STATUS_PENDING)
			},
			inviter: "nonexistent",
			wantErr: nil,
		},
		{
			name:    "EmptyResponseWhenNoInvitationsExist",
			setup:   func(f *fixture) {},
			inviter: "inviter1",
			wantErr: nil,
		},
		{
			name: "ReturnsInvitationWithRejectedStatus",
			setup: func(f *fixture) {
				createInvitationForInviter(f.keeper, f.ctx, 1, "inviterX", types.InvitationStatus_INVITATION_STATUS_REVOKED)
				createInvitationForInviter(f.keeper, f.ctx, 2, "inviterX", types.InvitationStatus_INVITATION_STATUS_ACCEPTED)
			},
			inviter:          "inviterX",
			wantInvitationID: 1,
			wantInviteeAddr:  "sprkdr1address",
			wantStatus:       uint64(types.InvitationStatus_INVITATION_STATUS_REVOKED),
		},
		{
			name:    "InvalidRequestNil",
			setup:   func(f *fixture) {},
			inviter: "",
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

			var req *types.QueryInvitationsByInviterRequest
			if tc.inviter != "" || tc.wantErr == nil {
				req = &types.QueryInvitationsByInviterRequest{Inviter: tc.inviter}
			}

			response, err := qs.InvitationsByInviter(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.wantInvitationID > 0 {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantInvitationID, response.InvitationId)
				require.Equal(t, tc.wantInviteeAddr, response.InviteeAddress)
				require.Equal(t, tc.wantStatus, response.Status)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, uint64(0), response.InvitationId)
				require.Equal(t, "", response.InviteeAddress)
				require.Equal(t, uint64(0), response.Status)
			}
		})
	}
}

func TestInvitationsByInviter_MultipleInvitations(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple invitations for the same inviter
	createInvitationForInviter(f.keeper, f.ctx, 1, "organizer1", types.InvitationStatus_INVITATION_STATUS_PENDING)
	createInvitationForInviter(f.keeper, f.ctx, 2, "organizer1", types.InvitationStatus_INVITATION_STATUS_ACCEPTED)
	createInvitationForInviter(f.keeper, f.ctx, 3, "organizer1", types.InvitationStatus_INVITATION_STATUS_PENDING)

	// Query should return first invitation (id 1)
	response, err := qs.InvitationsByInviter(f.ctx, &types.QueryInvitationsByInviterRequest{Inviter: "organizer1"})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, uint64(1), response.InvitationId)
	require.Equal(t, "sprkdr1address", response.InviteeAddress)
	require.Equal(t, uint64(types.InvitationStatus_INVITATION_STATUS_PENDING), response.Status)
}
