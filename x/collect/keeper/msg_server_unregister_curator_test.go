package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestUnregisterCurator(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture)
		msg            func(f *testFixture) *types.MsgUnregisterCurator
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture)
	}{
		{
			name: "success: curator with no pending challenges",
			setup: func(f *testFixture) {
				f.registerCurator(t, f.member, 500)
			},
			msg: func(f *testFixture) *types.MsgUnregisterCurator {
				return &types.MsgUnregisterCurator{
					Creator: f.member,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture) {
				_, err := f.keeper.Curator.Get(f.ctx, f.member)
				require.Error(t, err) // should be removed
			},
		},
		{
			name:  "error: curator not found",
			setup: func(f *testFixture) {},
			msg: func(f *testFixture) *types.MsgUnregisterCurator {
				return &types.MsgUnregisterCurator{
					Creator: f.member,
				}
			},
			expErr:         true,
			expErrContains: "not a registered active curator",
		},
		{
			name: "error: pending challenges > 0",
			setup: func(f *testFixture) {
				f.registerCurator(t, f.member, 500)
				// Set pending challenges
				curator, _ := f.keeper.Curator.Get(f.ctx, f.member)
				curator.PendingChallenges = 1
				f.keeper.Curator.Set(f.ctx, f.member, curator)
			},
			msg: func(f *testFixture) *types.MsgUnregisterCurator {
				return &types.MsgUnregisterCurator{
					Creator: f.member,
				}
			},
			expErr:         true,
			expErrContains: "pending challenges",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			tc.setup(f)
			msg := tc.msg(f)
			resp, err := f.msgServer.UnregisterCurator(f.ctx, msg)
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
				tc.check(t, f)
			}
		})
	}
}
