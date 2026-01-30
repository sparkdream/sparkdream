package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func createNVoteXpRecord(keeper keeper.Keeper, ctx context.Context, n int) []types.VoteXpRecord {
	items := make([]types.VoteXpRecord, n)
	for i := range items {
		items[i].SeasonMemberProposal = strconv.Itoa(i)
		items[i].GrantedBlock = int64(i)
		_ = keeper.VoteXpRecord.Set(ctx, items[i].SeasonMemberProposal, items[i])
	}
	return items
}

func TestVoteXpRecordQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVoteXpRecord(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetVoteXpRecordRequest
		response *types.QueryGetVoteXpRecordResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetVoteXpRecordRequest{
				SeasonMemberProposal: msgs[0].SeasonMemberProposal,
			},
			response: &types.QueryGetVoteXpRecordResponse{VoteXpRecord: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetVoteXpRecordRequest{
				SeasonMemberProposal: msgs[1].SeasonMemberProposal,
			},
			response: &types.QueryGetVoteXpRecordResponse{VoteXpRecord: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetVoteXpRecordRequest{
				SeasonMemberProposal: strconv.Itoa(100000),
			},
			err: status.Error(codes.NotFound, "not found"),
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetVoteXpRecord(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestVoteXpRecordQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVoteXpRecord(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllVoteXpRecordRequest {
		return &types.QueryAllVoteXpRecordRequest{
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
			resp, err := qs.ListVoteXpRecord(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VoteXpRecord), step)
			require.Subset(t, msgs, resp.VoteXpRecord)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListVoteXpRecord(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VoteXpRecord), step)
			require.Subset(t, msgs, resp.VoteXpRecord)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListVoteXpRecord(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.VoteXpRecord)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListVoteXpRecord(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
