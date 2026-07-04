package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
)

func TestQueryHideRecordsBySentinel(t *testing.T) {
	f := initTestFixture(t)

	// No hides yet.
	resp, err := f.queryServer.HideRecordsBySentinel(f.ctx, &types.QueryHideRecordsBySentinelRequest{
		Sentinel: f.sentinel,
	})
	require.NoError(t, err)
	require.Empty(t, resp.HideRecords)

	// Two hides by the sentinel on two collections.
	first := f.createCollection(t, f.owner)
	second := f.createCollection(t, f.owner)
	for _, id := range []uint64{first, second} {
		_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
			Creator:    f.sentinel,
			TargetId:   id,
			TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
		})
		require.NoError(t, err)
	}

	resp, err = f.queryServer.HideRecordsBySentinel(f.ctx, &types.QueryHideRecordsBySentinelRequest{
		Sentinel: f.sentinel,
	})
	require.NoError(t, err)
	require.Len(t, resp.HideRecords, 2)
	// Most recent hide first.
	require.Equal(t, second, resp.HideRecords[0].TargetId)
	require.Equal(t, first, resp.HideRecords[1].TargetId)
	for _, hr := range resp.HideRecords {
		require.Equal(t, f.sentinel, hr.Sentinel)
	}

	// A different account has no hides.
	resp, err = f.queryServer.HideRecordsBySentinel(f.ctx, &types.QueryHideRecordsBySentinelRequest{
		Sentinel: f.owner,
	})
	require.NoError(t, err)
	require.Empty(t, resp.HideRecords)
}

func TestQueryHideRecordsBySentinel_Pagination(t *testing.T) {
	f := initTestFixture(t)

	// Five hides by the sentinel; walk order is ascending id, response is
	// newest first, so targets come back in reverse creation order.
	targets := make([]uint64, 5)
	for i := range targets {
		targets[i] = f.createCollection(t, f.owner)
		_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
			Creator:    f.sentinel,
			TargetId:   targets[i],
			TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
		})
		require.NoError(t, err)
	}

	// First page of two: the two most recent hides, Total counts all matches.
	resp, err := f.queryServer.HideRecordsBySentinel(f.ctx, &types.QueryHideRecordsBySentinelRequest{
		Sentinel:   f.sentinel,
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp.HideRecords, 2)
	require.Equal(t, targets[4], resp.HideRecords[0].TargetId)
	require.Equal(t, targets[3], resp.HideRecords[1].TargetId)
	require.Equal(t, uint64(5), resp.Pagination.Total)

	// Offset into the tail: only the oldest hide remains.
	resp, err = f.queryServer.HideRecordsBySentinel(f.ctx, &types.QueryHideRecordsBySentinelRequest{
		Sentinel:   f.sentinel,
		Pagination: &query.PageRequest{Offset: 4, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp.HideRecords, 1)
	require.Equal(t, targets[0], resp.HideRecords[0].TargetId)
	require.Equal(t, uint64(5), resp.Pagination.Total)

	// Offset past the end yields an empty page, not an error.
	resp, err = f.queryServer.HideRecordsBySentinel(f.ctx, &types.QueryHideRecordsBySentinelRequest{
		Sentinel:   f.sentinel,
		Pagination: &query.PageRequest{Offset: 10},
	})
	require.NoError(t, err)
	require.Empty(t, resp.HideRecords)
	require.Equal(t, uint64(5), resp.Pagination.Total)

	// A limit above the 100 cap falls back to the default and returns all.
	resp, err = f.queryServer.HideRecordsBySentinel(f.ctx, &types.QueryHideRecordsBySentinelRequest{
		Sentinel:   f.sentinel,
		Pagination: &query.PageRequest{Limit: 200},
	})
	require.NoError(t, err)
	require.Len(t, resp.HideRecords, 5)
}

func TestQueryHideRecordsBySentinel_InvalidRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.HideRecordsBySentinel(f.ctx, nil)
	require.Error(t, err)

	_, err = f.queryServer.HideRecordsBySentinel(f.ctx, &types.QueryHideRecordsBySentinelRequest{
		Sentinel: "not-an-address",
	})
	require.Error(t, err)
}
