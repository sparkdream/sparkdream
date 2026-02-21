package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

func TestRegisterCurator(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture)
		msg            func(f *testFixture) *types.MsgRegisterCurator
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture)
	}{
		{
			name: "success: member registers with sufficient bond",
			setup: func(f *testFixture) {
				// Default mock returns ESTABLISHED trust level, which meets PROVISIONAL min
			},
			msg: func(f *testFixture) *types.MsgRegisterCurator {
				return &types.MsgRegisterCurator{
					Creator:    f.member,
					BondAmount: math.NewInt(500),
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture) {
				curator, err := f.keeper.Curator.Get(f.ctx, f.member)
				require.NoError(t, err)
				require.True(t, curator.Active)
				require.Equal(t, math.NewInt(500), curator.BondAmount)
			},
		},
		{
			name: "error: trust level too low",
			setup: func(f *testFixture) {
				f.repKeeper.getTrustLevelFn = func(_ context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error) {
					return reptypes.TrustLevel_TRUST_LEVEL_NEW, nil
				}
			},
			msg: func(f *testFixture) *types.MsgRegisterCurator {
				return &types.MsgRegisterCurator{
					Creator:    f.member,
					BondAmount: math.NewInt(500),
				}
			},
			expErr:         true,
			expErrContains: "below required trust level",
		},
		{
			name:  "error: bond below minimum",
			setup: func(f *testFixture) {},
			msg: func(f *testFixture) *types.MsgRegisterCurator {
				return &types.MsgRegisterCurator{
					Creator:    f.member,
					BondAmount: math.NewInt(100),
				}
			},
			expErr:         true,
			expErrContains: "bond below min curator bond",
		},
		{
			name: "error: already registered as active curator",
			setup: func(f *testFixture) {
				f.registerCurator(t, f.member, 500)
			},
			msg: func(f *testFixture) *types.MsgRegisterCurator {
				return &types.MsgRegisterCurator{
					Creator:    f.member,
					BondAmount: math.NewInt(500),
				}
			},
			expErr:         true,
			expErrContains: "already registered as curator",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			tc.setup(f)
			msg := tc.msg(f)
			resp, err := f.msgServer.RegisterCurator(f.ctx, msg)
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
