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

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func createNVotingProposal(keeper keeper.Keeper, ctx context.Context, n int) []types.VotingProposal {
	items := make([]types.VotingProposal, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].Title = strconv.Itoa(i)
		items[i].Description = strconv.Itoa(i)
		items[i].Proposer = strconv.Itoa(i)
		items[i].SnapshotBlock = int64(i)
		items[i].EligibleVoters = uint64(i)
		items[i].VotingStart = int64(i)
		items[i].VotingEnd = int64(i)
		items[i].Quorum = math.LegacyNewDec(int64(i))
		items[i].Threshold = math.LegacyNewDec(int64(i))
		items[i].VetoThreshold = math.LegacyNewDec(int64(i))
		items[i].Status = types.ProposalStatus(i)
		items[i].Outcome = types.ProposalOutcome(i)
		items[i].ProposalType = types.ProposalType(i)
		items[i].ReferenceId = uint64(i)
		items[i].CreatedAt = int64(i)
		items[i].FinalizedAt = int64(i)
		items[i].Visibility = types.VisibilityLevel(i)
		items[i].RevealEpoch = uint64(i)
		items[i].RevealEnd = int64(i)
		_ = keeper.VotingProposal.Set(ctx, iu, items[i])
		_ = keeper.VotingProposalSeq.Set(ctx, iu)
	}
	return items
}

func TestVotingProposalQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVotingProposal(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetVotingProposalRequest
		response *types.QueryGetVotingProposalResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetVotingProposalRequest{Id: msgs[0].Id},
			response: &types.QueryGetVotingProposalResponse{VotingProposal: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetVotingProposalRequest{Id: msgs[1].Id},
			response: &types.QueryGetVotingProposalResponse{VotingProposal: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetVotingProposalRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetVotingProposal(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestVotingProposalQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVotingProposal(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllVotingProposalRequest {
		return &types.QueryAllVotingProposalRequest{
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
			resp, err := qs.ListVotingProposal(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VotingProposal), step)
			require.Subset(t, msgs, resp.VotingProposal)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListVotingProposal(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VotingProposal), step)
			require.Subset(t, msgs, resp.VotingProposal)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListVotingProposal(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.VotingProposal)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListVotingProposal(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
