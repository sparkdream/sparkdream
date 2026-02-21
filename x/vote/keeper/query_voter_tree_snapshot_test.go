package keeper_test

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func createNVoterTreeSnapshot(keeper keeper.Keeper, ctx context.Context, n int) []types.VoterTreeSnapshot {
	items := make([]types.VoterTreeSnapshot, n)
	for i := range items {
		items[i].ProposalId = uint64(i)
		items[i].SnapshotBlock = int64(i)
		items[i].VoterCount = uint64(i)
		_ = keeper.VoterTreeSnapshot.Set(ctx, items[i].ProposalId, items[i])
	}
	return items
}

func TestVoterTreeSnapshotQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVoterTreeSnapshot(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetVoterTreeSnapshotRequest
		response *types.QueryGetVoterTreeSnapshotResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetVoterTreeSnapshotRequest{
				ProposalId: msgs[0].ProposalId,
			},
			response: &types.QueryGetVoterTreeSnapshotResponse{VoterTreeSnapshot: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetVoterTreeSnapshotRequest{
				ProposalId: msgs[1].ProposalId,
			},
			response: &types.QueryGetVoterTreeSnapshotResponse{VoterTreeSnapshot: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetVoterTreeSnapshotRequest{
				ProposalId: 100000,
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
			response, err := qs.GetVoterTreeSnapshot(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestVoterTreeSnapshotQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNVoterTreeSnapshot(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllVoterTreeSnapshotRequest {
		return &types.QueryAllVoterTreeSnapshotRequest{
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
			resp, err := qs.ListVoterTreeSnapshot(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VoterTreeSnapshot), step)
			require.Subset(t, msgs, resp.VoterTreeSnapshot)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListVoterTreeSnapshot(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.VoterTreeSnapshot), step)
			require.Subset(t, msgs, resp.VoterTreeSnapshot)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListVoterTreeSnapshot(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.VoterTreeSnapshot)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListVoterTreeSnapshot(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
