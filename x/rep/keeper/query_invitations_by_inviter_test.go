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
		name    string
		setup   func(*fixture)
		inviter string
		wantIDs []uint64
		wantErr error
	}{
		{
			name: "ReturnsAllInvitationsForInviter",
			setup: func(f *fixture) {
				createInvitationForInviter(f.keeper, f.ctx, 1, "inviter1", types.InvitationStatus_INVITATION_STATUS_PENDING)
				createInvitationForInviter(f.keeper, f.ctx, 2, "inviter2", types.InvitationStatus_INVITATION_STATUS_PENDING)
				createInvitationForInviter(f.keeper, f.ctx, 3, "inviter1", types.InvitationStatus_INVITATION_STATUS_ACCEPTED)
			},
			inviter: "inviter1",
			wantIDs: []uint64{1, 3},
		},
		{
			name: "EmptyResponseWhenNoInvitationsForInviter",
			setup: func(f *fixture) {
				createInvitationForInviter(f.keeper, f.ctx, 1, "inviter1", types.InvitationStatus_INVITATION_STATUS_PENDING)
				createInvitationForInviter(f.keeper, f.ctx, 2, "inviter2", types.InvitationStatus_INVITATION_STATUS_PENDING)
			},
			inviter: "nonexistent",
			wantIDs: nil,
		},
		{
			name:    "EmptyResponseWhenNoInvitationsExist",
			setup:   func(f *fixture) {},
			inviter: "inviter1",
			wantIDs: nil,
		},
		{
			name: "IncludesRevokedInvitations",
			setup: func(f *fixture) {
				createInvitationForInviter(f.keeper, f.ctx, 1, "inviterX", types.InvitationStatus_INVITATION_STATUS_REVOKED)
				createInvitationForInviter(f.keeper, f.ctx, 2, "inviterX", types.InvitationStatus_INVITATION_STATUS_ACCEPTED)
			},
			inviter: "inviterX",
			wantIDs: []uint64{1, 2},
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
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			gotIDs := make([]uint64, 0, len(response.Invitation))
			for _, inv := range response.Invitation {
				require.Equal(t, tc.inviter, inv.Inviter)
				gotIDs = append(gotIDs, inv.Id)
			}
			require.ElementsMatch(t, tc.wantIDs, gotIDs)
		})
	}
}

func TestInvitationsByInviter_Pagination(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	createInvitationForInviter(f.keeper, f.ctx, 1, "organizer1", types.InvitationStatus_INVITATION_STATUS_PENDING)
	createInvitationForInviter(f.keeper, f.ctx, 2, "organizer1", types.InvitationStatus_INVITATION_STATUS_ACCEPTED)
	createInvitationForInviter(f.keeper, f.ctx, 3, "other", types.InvitationStatus_INVITATION_STATUS_PENDING)
	createInvitationForInviter(f.keeper, f.ctx, 4, "organizer1", types.InvitationStatus_INVITATION_STATUS_PENDING)

	// First page of 2
	response, err := qs.InvitationsByInviter(f.ctx, &types.QueryInvitationsByInviterRequest{
		Inviter:    "organizer1",
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, response.Invitation, 2)
	require.Equal(t, uint64(1), response.Invitation[0].Id)
	require.Equal(t, uint64(2), response.Invitation[1].Id)
	require.NotNil(t, response.Pagination)
	require.NotEmpty(t, response.Pagination.NextKey)

	// Second page
	response, err = qs.InvitationsByInviter(f.ctx, &types.QueryInvitationsByInviterRequest{
		Inviter:    "organizer1",
		Pagination: &query.PageRequest{Limit: 2, Key: response.Pagination.NextKey},
	})
	require.NoError(t, err)
	require.Len(t, response.Invitation, 1)
	require.Equal(t, uint64(4), response.Invitation[0].Id)
	require.Empty(t, response.Pagination.NextKey)
}
