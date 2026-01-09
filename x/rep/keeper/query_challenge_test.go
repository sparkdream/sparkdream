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

func createNChallenge(keeper keeper.Keeper, ctx context.Context, n int) []types.Challenge {
	items := make([]types.Challenge, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].InitiativeId = uint64(i)
		items[i].Challenger = strconv.Itoa(i)
		items[i].Reason = strconv.Itoa(i)
		amount := math.NewInt(int64(i))
		items[i].StakedDream = &amount
		items[i].IsAnonymous = true
		items[i].PayoutAddress = strconv.Itoa(i)
		items[i].Status = types.ChallengeStatus(i)
		items[i].CreatedAt = int64(i)
		items[i].ResolvedAt = int64(i)
		_ = keeper.Challenge.Set(ctx, iu, items[i])
		_ = keeper.ChallengeSeq.Set(ctx, iu)
	}
	return items
}

func TestChallengeQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNChallenge(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetChallengeRequest
		response *types.QueryGetChallengeResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetChallengeRequest{Id: msgs[0].Id},
			response: &types.QueryGetChallengeResponse{Challenge: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetChallengeRequest{Id: msgs[1].Id},
			response: &types.QueryGetChallengeResponse{Challenge: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetChallengeRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetChallenge(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestChallengeQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNChallenge(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllChallengeRequest {
		return &types.QueryAllChallengeRequest{
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
			resp, err := qs.ListChallenge(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Challenge), step)
			require.Subset(t, msgs, resp.Challenge)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListChallenge(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Challenge), step)
			require.Subset(t, msgs, resp.Challenge)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListChallenge(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Challenge)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListChallenge(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
