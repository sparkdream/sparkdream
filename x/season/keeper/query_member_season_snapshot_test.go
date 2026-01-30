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

func createNMemberSeasonSnapshot(keeper keeper.Keeper, ctx context.Context, n int) []types.MemberSeasonSnapshot {
	items := make([]types.MemberSeasonSnapshot, n)
	for i := range items {
		items[i].SeasonAddress = strconv.Itoa(i)
		items[i].FinalDreamBalance = strconv.Itoa(i)
		items[i].InitiativesCompleted = uint64(i)
		items[i].XpEarned = uint64(i)
		items[i].SeasonLevel = uint64(i)
		_ = keeper.MemberSeasonSnapshot.Set(ctx, items[i].SeasonAddress, items[i])
	}
	return items
}

func TestMemberSeasonSnapshotQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberSeasonSnapshot(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetMemberSeasonSnapshotRequest
		response *types.QueryGetMemberSeasonSnapshotResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetMemberSeasonSnapshotRequest{
				SeasonAddress: msgs[0].SeasonAddress,
			},
			response: &types.QueryGetMemberSeasonSnapshotResponse{MemberSeasonSnapshot: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetMemberSeasonSnapshotRequest{
				SeasonAddress: msgs[1].SeasonAddress,
			},
			response: &types.QueryGetMemberSeasonSnapshotResponse{MemberSeasonSnapshot: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetMemberSeasonSnapshotRequest{
				SeasonAddress: strconv.Itoa(100000),
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
			response, err := qs.GetMemberSeasonSnapshot(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestMemberSeasonSnapshotQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMemberSeasonSnapshot(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllMemberSeasonSnapshotRequest {
		return &types.QueryAllMemberSeasonSnapshotRequest{
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
			resp, err := qs.ListMemberSeasonSnapshot(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberSeasonSnapshot), step)
			require.Subset(t, msgs, resp.MemberSeasonSnapshot)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListMemberSeasonSnapshot(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.MemberSeasonSnapshot), step)
			require.Subset(t, msgs, resp.MemberSeasonSnapshot)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListMemberSeasonSnapshot(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.MemberSeasonSnapshot)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListMemberSeasonSnapshot(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
