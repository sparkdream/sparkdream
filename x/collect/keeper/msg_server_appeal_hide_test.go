package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
)

func TestAppealHide(t *testing.T) {
	// Helper: create a hidden collection and return (collectionID, hideRecordID).
	hideCollection := func(t *testing.T, f *testFixture) (uint64, uint64) {
		t.Helper()
		collID := f.createCollection(t, f.owner)
		resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
			Creator:    f.sentinel,
			TargetId:   collID,
			TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
			ReasonText: "spam",
		})
		require.NoError(t, err)
		return collID, resp.HideRecordId
	}

	tests := []struct {
		name           string
		setup          func(f *testFixture) (uint64, string) // returns (hideRecordID, creator)
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, hideRecordID uint64)
	}{
		{
			name: "success owner appeals",
			setup: func(f *testFixture) (uint64, string) {
				_, hrID := hideCollection(t, f)
				// Advance past appeal cooldown
				params, _ := f.keeper.Params.Get(f.ctx)
				f.advanceBlockHeight(params.AppealCooldownBlocks + 1)
				return hrID, f.owner
			},
			check: func(t *testing.T, f *testFixture, hideRecordID uint64) {
				hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
				require.NoError(t, err)
				require.True(t, hr.Appealed)
			},
		},
		{
			name: "error not content owner",
			setup: func(f *testFixture) (uint64, string) {
				_, hrID := hideCollection(t, f)
				params, _ := f.keeper.Params.Get(f.ctx)
				f.advanceBlockHeight(params.AppealCooldownBlocks + 1)
				return hrID, f.member // member is not the owner
			},
			expErr:         true,
			expErrContains: "only content owner",
		},
		{
			name: "error already appealed",
			setup: func(f *testFixture) (uint64, string) {
				_, hrID := hideCollection(t, f)
				params, _ := f.keeper.Params.Get(f.ctx)
				f.advanceBlockHeight(params.AppealCooldownBlocks + 1)
				// First appeal succeeds
				_, err := f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{
					Creator:      f.owner,
					HideRecordId: hrID,
				})
				require.NoError(t, err)
				return hrID, f.owner
			},
			expErr:         true,
			expErrContains: "appeal already filed",
		},
		{
			name: "error cooldown not elapsed",
			setup: func(f *testFixture) (uint64, string) {
				_, hrID := hideCollection(t, f)
				// Do NOT advance past cooldown
				return hrID, f.owner
			},
			expErr:         true,
			expErrContains: "appeal cooldown not elapsed",
		},
		{
			name: "error deadline passed",
			setup: func(f *testFixture) (uint64, string) {
				_, hrID := hideCollection(t, f)
				// Advance past appeal deadline
				params, _ := f.keeper.Params.Get(f.ctx)
				f.advanceBlockHeight(params.HideExpiryBlocks + 1)
				return hrID, f.owner
			},
			expErr:         true,
			expErrContains: "resolved",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			hideRecordID, creator := tc.setup(f)

			resp, err := f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{
				Creator:      creator,
				HideRecordId: hideRecordID,
			})

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
				tc.check(t, f, hideRecordID)
			}
		})
	}
}
