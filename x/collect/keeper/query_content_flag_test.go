package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryContentFlag(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		req    func(collID uint64) *types.QueryContentFlagRequest
		expErr bool
		check  func(t *testing.T, resp *types.QueryContentFlagResponse, collID uint64)
	}{
		{
			name: "found after flagging",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				_, err := f.msgServer.FlagContent(f.ctx, &types.MsgFlagContent{
					Creator:    f.member,
					TargetId:   collID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     types.ModerationReason_MODERATION_REASON_SPAM,
				})
				require.NoError(t, err)
				return collID
			},
			req: func(collID uint64) *types.QueryContentFlagRequest {
				return &types.QueryContentFlagRequest{
					TargetId:   collID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
				}
			},
			expErr: false,
			check: func(t *testing.T, resp *types.QueryContentFlagResponse, collID uint64) {
				require.Equal(t, collID, resp.CollectionFlag.TargetId)
				require.Len(t, resp.CollectionFlag.FlagRecords, 1)
			},
		},
		{
			name:  "not found",
			setup: nil,
			req: func(_ uint64) *types.QueryContentFlagRequest {
				return &types.QueryContentFlagRequest{
					TargetId:   999,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
				}
			},
			expErr: true,
		},
		{
			name:  "nil request",
			setup: nil,
			req: func(_ uint64) *types.QueryContentFlagRequest {
				return nil
			},
			expErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			var collID uint64
			if tc.setup != nil {
				collID = tc.setup(f)
			}
			resp, err := f.queryServer.ContentFlag(f.ctx, tc.req(collID))
			if tc.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, resp, collID)
			}
		})
	}
}
