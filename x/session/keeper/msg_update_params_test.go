package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestMsgUpdateParams(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	params := types.DefaultParams()
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	testCases := []struct {
		name      string
		input     *types.MsgUpdateParams
		expErr    bool
		expErrMsg string
	}{
		{
			name: "invalid authority",
			input: &types.MsgUpdateParams{
				Authority: "invalid",
				Params:    params,
			},
			expErr:    true,
			expErrMsg: "invalid authority",
		},
		{
			name: "empty params fails validation",
			input: &types.MsgUpdateParams{
				Authority: authorityStr,
				Params:    types.Params{},
			},
			expErr:    true,
			expErrMsg: "max_sessions_per_granter must be > 0",
		},
		{
			name: "all good",
			input: &types.MsgUpdateParams{
				Authority: authorityStr,
				Params:    params,
			},
			expErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ms.UpdateParams(f.ctx, tc.input)

			if tc.expErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgUpdateParamsCeilingEnforcement(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	// Set default params
	require.NoError(t, f.keeper.Params.Set(f.ctx, types.DefaultParams()))

	tests := []struct {
		name        string
		params      types.Params
		expectError bool
		errContains string
	}{
		{
			name: "shrinking ceiling is allowed",
			params: types.NewParams(
				types.DefaultAllowedMsgTypes[:5], // shrink ceiling
				types.DefaultAllowedMsgTypes[:3], // active subset
				10, 20,
				7*24*time.Hour,
				sdk.NewInt64Coin("uspark", 100_000_000),
			),
			expectError: false,
		},
		{
			name: "expanding ceiling is forbidden",
			params: types.NewParams(
				append(types.DefaultAllowedMsgTypes, "/sparkdream.new.v1.MsgNew"), // expand
				types.DefaultAllowedMsgTypes[:3],
				10, 20,
				7*24*time.Hour,
				sdk.NewInt64Coin("uspark", 100_000_000),
			),
			expectError: true,
			errContains: "not in current ceiling",
		},
		{
			name:        "same ceiling is allowed",
			params:      types.DefaultParams(),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset to defaults before each test
			require.NoError(t, f.keeper.Params.Set(f.ctx, types.DefaultParams()))

			_, err := ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
				Authority: authorityStr,
				Params:    tt.params,
			})

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgUpdateParamsUnauthorized(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	randomAddr := testAddr("random", f.addressCodec)
	_, err := ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: randomAddr,
		Params:    types.DefaultParams(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority")
}
