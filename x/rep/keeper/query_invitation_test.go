package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createNInvitation(keeper keeper.Keeper, ctx context.Context, n int) []types.Invitation {
	items := make([]types.Invitation, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].Inviter = strconv.Itoa(i)
		items[i].InviteeAddress = strconv.Itoa(i)
		amount := math.NewInt(int64(i))
		items[i].StakedDream = &amount
		items[i].AccountabilityEnd = int64(i)
		dec := math.LegacyNewDec(int64(i))
		items[i].ReferralRate = &dec
		items[i].ReferralEnd = int64(i)
		items[i].ReferralEarned = &amount
		items[i].Status = types.InvitationStatus(i)
		items[i].CreatedAt = int64(i)
		items[i].AcceptedAt = int64(i)
		_ = keeper.Invitation.Set(ctx, iu, items[i])
		_ = keeper.InvitationSeq.Set(ctx, iu)
	}
	return items
}

func TestInvitationQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNInvitation(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetInvitationRequest
		response *types.QueryGetInvitationResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetInvitationRequest{Id: msgs[0].Id},
			response: &types.QueryGetInvitationResponse{Invitation: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetInvitationRequest{Id: msgs[1].Id},
			response: &types.QueryGetInvitationResponse{Invitation: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetInvitationRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetInvitation(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestInvitationQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNInvitation(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllInvitationRequest {
		return &types.QueryAllInvitationRequest{
			Pagination: &query.PageRequest{
				Key:        next,
				Offset:     offset,
				Limit:      limit,
				CountTotal: total,
			},
		}
	}
	t.Run("ByOffset", func(t *testing.T) {
		step := 2
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListInvitation(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Invitation), step)
			require.Subset(t, msgs, resp.Invitation)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListInvitation(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Invitation), step)
			require.Subset(t, msgs, resp.Invitation)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListInvitation(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Invitation)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListInvitation(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
