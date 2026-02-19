package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryHideRecord(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		req    func(id uint64) *types.QueryHideRecordRequest
		expErr bool
		check  func(t *testing.T, resp *types.QueryHideRecordResponse, id uint64)
	}{
		{
			name: "found after hiding",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
					Creator:    f.sentinel,
					TargetId:   collID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					ReasonCode: types.ModerationReason_MODERATION_REASON_SPAM,
				})
				require.NoError(t, err)
				return resp.HideRecordId
			},
			req: func(id uint64) *types.QueryHideRecordRequest {
				return &types.QueryHideRecordRequest{Id: id}
			},
			expErr: false,
			check: func(t *testing.T, resp *types.QueryHideRecordResponse, id uint64) {
				require.Equal(t, id, resp.HideRecord.Id)
				require.Equal(t, types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, resp.HideRecord.TargetType)
			},
		},
		{
			name:   "not found",
			setup:  nil,
			req:    func(_ uint64) *types.QueryHideRecordRequest { return &types.QueryHideRecordRequest{Id: 999} },
			expErr: true,
		},
		{
			name:   "nil request",
			setup:  nil,
			req:    func(_ uint64) *types.QueryHideRecordRequest { return nil },
			expErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			var id uint64
			if tc.setup != nil {
				id = tc.setup(f)
			}
			resp, err := f.queryServer.HideRecord(f.ctx, tc.req(id))
			if tc.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, resp, id)
			}
		})
	}
}
