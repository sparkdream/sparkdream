package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
)

func TestFlagContent(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, targetID uint64) *types.MsgFlagContent
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, targetID uint64)
	}{
		{
			name: "success flag with valid reason",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, targetID uint64) *types.MsgFlagContent {
				return &types.MsgFlagContent{
					Creator:    f.member,
					TargetId:   targetID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_SPAM,
				}
			},
			check: func(t *testing.T, f *testFixture, targetID uint64) {
				flagKey := keeper.FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, targetID)
				flag, err := f.keeper.Flag.Get(f.ctx, flagKey)
				require.NoError(t, err)
				require.Len(t, flag.FlagRecords, 1)
				require.Equal(t, f.member, flag.FlagRecords[0].Flagger)
				require.Equal(t, commontypes.ModerationReason_MODERATION_REASON_SPAM, flag.FlagRecords[0].Reason)
			},
		},
		{
			name: "success weight reaches threshold sets in_review_queue",
			setup: func(f *testFixture) uint64 {
				// Set threshold to 4 so two flags (weight 2 each) will reach it
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				params.FlagReviewThreshold = 4
				require.NoError(t, f.keeper.Params.Set(f.ctx, params))

				collID := f.createCollection(t, f.owner)

				// First flag from sentinel (also a member)
				_, err = f.msgServer.FlagContent(f.ctx, &types.MsgFlagContent{
					Creator:    f.sentinel,
					TargetId:   collID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_SPAM,
				})
				require.NoError(t, err)

				return collID
			},
			msg: func(f *testFixture, targetID uint64) *types.MsgFlagContent {
				return &types.MsgFlagContent{
					Creator:    f.member,
					TargetId:   targetID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_INAPPROPRIATE,
				}
			},
			check: func(t *testing.T, f *testFixture, targetID uint64) {
				flagKey := keeper.FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, targetID)
				flag, err := f.keeper.Flag.Get(f.ctx, flagKey)
				require.NoError(t, err)
				require.True(t, flag.InReviewQueue)
				require.Len(t, flag.FlagRecords, 2)
			},
		},
		{
			name: "error not member",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, targetID uint64) *types.MsgFlagContent {
				return &types.MsgFlagContent{
					Creator:    f.nonMember,
					TargetId:   targetID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_SPAM,
				}
			},
			expErr:         true,
			expErrContains: "not an active x/rep member",
		},
		{
			name: "error already flagged",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				_, err := f.msgServer.FlagContent(f.ctx, &types.MsgFlagContent{
					Creator:    f.member,
					TargetId:   collID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_SPAM,
				})
				require.NoError(t, err)
				return collID
			},
			msg: func(f *testFixture, targetID uint64) *types.MsgFlagContent {
				return &types.MsgFlagContent{
					Creator:    f.member,
					TargetId:   targetID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_SPAM,
				}
			},
			expErr:         true,
			expErrContains: "already flagged",
		},
		{
			name: "error REASON_UNSPECIFIED",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, targetID uint64) *types.MsgFlagContent {
				return &types.MsgFlagContent{
					Creator:    f.member,
					TargetId:   targetID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_UNSPECIFIED,
				}
			},
			expErr:         true,
			expErrContains: "invalid or unspecified flag reason",
		},
		{
			name: "error REASON_OTHER without text",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, targetID uint64) *types.MsgFlagContent {
				return &types.MsgFlagContent{
					Creator:    f.member,
					TargetId:   targetID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_OTHER,
					ReasonText: "",
				}
			},
			expErr:         true,
			expErrContains: "reason_text required",
		},
		{
			name: "success REASON_OTHER with text",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, targetID uint64) *types.MsgFlagContent {
				return &types.MsgFlagContent{
					Creator:    f.member,
					TargetId:   targetID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_OTHER,
					ReasonText: "custom reason text",
				}
			},
			check: func(t *testing.T, f *testFixture, targetID uint64) {
				flagKey := keeper.FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, targetID)
				flag, err := f.keeper.Flag.Get(f.ctx, flagKey)
				require.NoError(t, err)
				require.Equal(t, "custom reason text", flag.FlagRecords[0].ReasonText)
			},
		},
		{
			name: "error non-OTHER reason with reason_text",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, targetID uint64) *types.MsgFlagContent {
				return &types.MsgFlagContent{
					Creator:    f.member,
					TargetId:   targetID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
					Reason:     commontypes.ModerationReason_MODERATION_REASON_SPAM,
					ReasonText: "should not be set",
				}
			},
			expErr:         true,
			expErrContains: "invalid or unspecified flag reason",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			var targetID uint64
			if tc.setup != nil {
				targetID = tc.setup(f)
			}

			msg := tc.msg(f, targetID)
			resp, err := f.msgServer.FlagContent(f.ctx, msg)

			if tc.expErr {
				require.Error(t, err)
				if tc.expErrContains != "" {
					require.Contains(t, err.Error(), tc.expErrContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)

			if tc.check != nil {
				tc.check(t, f, targetID)
			}
		})
	}
}
