package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestUpdateOperationalParams(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture)
		msg            func(f *testFixture) *types.MsgUpdateOperationalParams
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture)
	}{
		{
			name: "success authorized updates deposit params",
			msg: func(f *testFixture) *types.MsgUpdateOperationalParams {
				return &types.MsgUpdateOperationalParams{
					Authority: f.authority,
					OperationalParams: types.CollectOperationalParams{
						BaseCollectionDeposit: math.NewInt(2000000),
						PerItemDeposit:        math.NewInt(200000),
					},
				}
			},
			check: func(t *testing.T, f *testFixture) {
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				require.Equal(t, math.NewInt(2000000), params.BaseCollectionDeposit)
				require.Equal(t, math.NewInt(200000), params.PerItemDeposit)
			},
		},
		{
			name: "success council authorized updates",
			setup: func(f *testFixture) {
				f.commonsKeeper.isCouncilAuthorizedFn = func(_ context.Context, addr string, _ string, _ string) bool {
					return addr == f.member
				}
			},
			msg: func(f *testFixture) *types.MsgUpdateOperationalParams {
				return &types.MsgUpdateOperationalParams{
					Authority: f.member,
					OperationalParams: types.CollectOperationalParams{
						DownvoteCost: math.NewInt(50000000),
					},
				}
			},
			check: func(t *testing.T, f *testFixture) {
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				require.Equal(t, math.NewInt(50000000), params.DownvoteCost)
			},
		},
		{
			name: "error unauthorized",
			setup: func(f *testFixture) {
				f.commonsKeeper.isCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
					return false
				}
			},
			msg: func(f *testFixture) *types.MsgUpdateOperationalParams {
				return &types.MsgUpdateOperationalParams{
					Authority: f.member,
					OperationalParams: types.CollectOperationalParams{
						DownvoteCost: math.NewInt(50000000),
					},
				}
			},
			expErr:         true,
			expErrContains: "not authorized",
		},
		{
			name: "error invalid params - negative deposit",
			msg: func(f *testFixture) *types.MsgUpdateOperationalParams {
				return &types.MsgUpdateOperationalParams{
					Authority: f.authority,
					OperationalParams: types.CollectOperationalParams{
						BaseCollectionDeposit: math.NewInt(-1),
					},
				}
			},
			expErr:         true,
			expErrContains: "must be positive",
		},
		{
			name: "success preserves non-operational params",
			msg: func(f *testFixture) *types.MsgUpdateOperationalParams {
				return &types.MsgUpdateOperationalParams{
					Authority: f.authority,
					OperationalParams: types.CollectOperationalParams{
						SponsorFee: math.NewInt(5000000),
					},
				}
			},
			check: func(t *testing.T, f *testFixture) {
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				// Operational field was updated
				require.Equal(t, math.NewInt(5000000), params.SponsorFee)
				// Non-operational fields preserved
				require.Equal(t, types.DefaultMaxTitleLength, params.MaxTitleLength)
				require.Equal(t, types.DefaultMaxItemsPerCollection, params.MaxItemsPerCollection)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			if tc.setup != nil {
				tc.setup(f)
			}

			msg := tc.msg(f)
			resp, err := f.msgServer.UpdateOperationalParams(f.ctx, msg)

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
