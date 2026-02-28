package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
)

func TestQueryHideRecordsByTarget(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		expLen int
	}{
		{
			name: "empty - no hide records",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			expLen: 0,
		},
		{
			name: "returns hide record after hiding",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
					Creator:    f.sentinel,
					TargetId:   collID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
				})
				require.NoError(t, err)
				return collID
			},
			expLen: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID := tc.setup(f)
			resp, err := f.queryServer.HideRecordsByTarget(f.ctx, &types.QueryHideRecordsByTargetRequest{
				TargetId:   collID,
				TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.HideRecords, tc.expLen)
		})
	}
}

func TestQueryHideRecordsByTarget_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.HideRecordsByTarget(f.ctx, nil)
	require.Error(t, err)
}
