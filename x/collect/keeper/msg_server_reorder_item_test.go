package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestReorderItem(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) (uint64, uint64, uint64) // returns collID, item1ID, item2ID
		msg            func(f *testFixture, collID, item1ID, item2ID uint64) *types.MsgReorderItem
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID, item1ID, item2ID uint64)
	}{
		{
			name: "success: move item forward",
			setup: func(f *testFixture) (uint64, uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				item1 := f.addItem(t, collID, f.owner)
				item2 := f.addItem(t, collID, f.owner)
				item3 := f.addItem(t, collID, f.owner)
				_ = item3
				return collID, item1, item2
			},
			msg: func(f *testFixture, collID, item1ID, item2ID uint64) *types.MsgReorderItem {
				return &types.MsgReorderItem{
					Creator:     f.owner,
					Id:          item1ID,
					NewPosition: 2,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID, item1ID, item2ID uint64) {
				item, err := f.keeper.Item.Get(f.ctx, item1ID)
				require.NoError(t, err)
				require.Equal(t, uint64(2), item.Position)
			},
		},
		{
			name: "no-op: same position",
			setup: func(f *testFixture) (uint64, uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				item1 := f.addItem(t, collID, f.owner)
				return collID, item1, 0
			},
			msg: func(f *testFixture, collID, item1ID, item2ID uint64) *types.MsgReorderItem {
				return &types.MsgReorderItem{
					Creator:     f.owner,
					Id:          item1ID,
					NewPosition: 0,
				}
			},
			expErr: false,
		},
		{
			name: "error: position out of range",
			setup: func(f *testFixture) (uint64, uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				item1 := f.addItem(t, collID, f.owner)
				return collID, item1, 0
			},
			msg: func(f *testFixture, collID, item1ID, item2ID uint64) *types.MsgReorderItem {
				return &types.MsgReorderItem{
					Creator:     f.owner,
					Id:          item1ID,
					NewPosition: 999,
				}
			},
			expErr:         true,
			expErrContains: "position out of range",
		},
		{
			name: "error: unauthorized non-writer",
			setup: func(f *testFixture) (uint64, uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				item1 := f.addItem(t, collID, f.owner)
				return collID, item1, 0
			},
			msg: func(f *testFixture, collID, item1ID, item2ID uint64) *types.MsgReorderItem {
				return &types.MsgReorderItem{
					Creator:     f.member,
					Id:          item1ID,
					NewPosition: 0,
				}
			},
			expErr:         true,
			expErrContains: "unauthorized",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID, item1ID, item2ID := tc.setup(f)
			msg := tc.msg(f, collID, item1ID, item2ID)
			resp, err := f.msgServer.ReorderItem(f.ctx, msg)
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
				tc.check(t, f, collID, item1ID, item2ID)
			}
		})
	}
}
