package keeper_test

import (
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// createNSentinelActivity seeds n sentinels across BOTH state owners the
// projection query composes: forum's slim store gets the forum-local fields
// (pending_hide_count here) and the mock rep keeper gets a RoleActivity
// carrying everything that migrated. The returned records are the EXPECTED
// projection outputs.
func createNSentinelActivity(f *fixture, n int) []types.SentinelActivity {
	items := make([]types.SentinelActivity, n)
	for i := range items {
		addr := strconv.Itoa(i)
		v := uint64(i)

		// Forum-local slice.
		_ = f.keeper.SentinelActivity.Set(f.ctx, addr, types.SentinelActivity{
			Address:          addr,
			PendingHideCount: v,
		})

		// Rep-side shared slice (mock).
		if f.repKeeper.roleActivities == nil {
			f.repKeeper.roleActivities = map[string]reptypes.RoleActivity{}
		}
		f.repKeeper.roleActivities[addr] = reptypes.RoleActivity{
			RoleType:              reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL,
			Address:               addr,
			ConsecutiveUpheld:     v,
			ConsecutiveOverturns:  v,
			OverturnCooldownUntil: int64(i),
			EpochAppealsResolved:  v,
			TotalActions: map[string]uint64{
				reptypes.ActionKindForumHide: v, reptypes.ActionKindForumLock: v,
				reptypes.ActionKindForumMove: v, reptypes.ActionKindForumPin: v,
			},
			UpheldActions: map[string]uint64{
				reptypes.ActionKindForumHide: v, reptypes.ActionKindForumLock: v,
				reptypes.ActionKindForumMove: v, reptypes.ActionKindForumPin: v,
			},
			OverturnedActions: map[string]uint64{
				reptypes.ActionKindForumHide: v, reptypes.ActionKindForumLock: v,
				reptypes.ActionKindForumMove: v, reptypes.ActionKindForumPin: v,
			},
			EpochActions: map[string]uint64{
				reptypes.ActionKindForumHide: v, reptypes.ActionKindForumLock: v,
				reptypes.ActionKindForumMove: v, reptypes.ActionKindForumPin: v,
				reptypes.ActionKindForumAppealFiled: v,
			},
		}

		// Expected projection output.
		items[i] = types.SentinelActivity{
			Address:               addr,
			PendingHideCount:      v,
			TotalHides:            v,
			UpheldHides:           v,
			OverturnedHides:       v,
			EpochHides:            v,
			TotalLocks:            v,
			UpheldLocks:           v,
			OverturnedLocks:       v,
			EpochLocks:            v,
			TotalMoves:            v,
			UpheldMoves:           v,
			OverturnedMoves:       v,
			EpochMoves:            v,
			TotalPins:             v,
			UpheldPins:            v,
			OverturnedPins:        v,
			EpochPins:             v,
			EpochAppealsFiled:     v,
			EpochAppealsResolved:  v,
			ConsecutiveUpheld:     v,
			ConsecutiveOverturns:  v,
			OverturnCooldownUntil: int64(i),
			AccuracyWindow:        []*types.AccuracyEpochBucket{},
		}
	}
	return items
}

func TestSentinelActivityQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNSentinelActivity(f, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetSentinelActivityRequest
		response *types.QueryGetSentinelActivityResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetSentinelActivityRequest{
				Address: msgs[0].Address,
			},
			response: &types.QueryGetSentinelActivityResponse{SentinelActivity: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetSentinelActivityRequest{
				Address: msgs[1].Address,
			},
			response: &types.QueryGetSentinelActivityResponse{SentinelActivity: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetSentinelActivityRequest{
				Address: strconv.Itoa(100000),
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
			response, err := qs.GetSentinelActivity(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestSentinelActivityQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNSentinelActivity(f, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllSentinelActivityRequest {
		return &types.QueryAllSentinelActivityRequest{
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
			resp, err := qs.ListSentinelActivity(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.SentinelActivity), step)
			require.Subset(t, msgs, resp.SentinelActivity)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListSentinelActivity(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.SentinelActivity), step)
			require.Subset(t, msgs, resp.SentinelActivity)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListSentinelActivity(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.SentinelActivity)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListSentinelActivity(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

// TestSentinelActivityQuery_CollectOnlySentinel: a sentinel who has only
// moderated on other surfaces (collect) has no forum-local record; the Get
// projection must still surface the shared rep-side data instead of
// returning NotFound.
func TestSentinelActivityQuery_CollectOnlySentinel(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	const addr = "collect-only-sentinel"

	if f.repKeeper.roleActivities == nil {
		f.repKeeper.roleActivities = map[string]reptypes.RoleActivity{}
	}
	f.repKeeper.roleActivities[addr] = reptypes.RoleActivity{
		RoleType:             reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL,
		Address:              addr,
		ConsecutiveUpheld:    2,
		EpochAppealsResolved: 2,
		TotalActions:         map[string]uint64{reptypes.ActionKindCollectHide: 4},
		UpheldActions:        map[string]uint64{reptypes.ActionKindCollectHide: 2},
		EpochActions:         map[string]uint64{reptypes.ActionKindCollectHide: 1},
	}

	resp, err := qs.GetSentinelActivity(f.ctx, &types.QueryGetSentinelActivityRequest{Address: addr})
	require.NoError(t, err, "rep-side activity alone must satisfy the query")
	require.Equal(t, uint64(4), resp.SentinelActivity.TotalCollectHides)
	require.Equal(t, uint64(2), resp.SentinelActivity.UpheldCollectHides)
	require.Equal(t, uint64(1), resp.SentinelActivity.EpochCollectHides)
	require.Equal(t, uint64(2), resp.SentinelActivity.ConsecutiveUpheld)
	require.Equal(t, uint64(0), resp.SentinelActivity.PendingHideCount, "no forum-local record")
}
