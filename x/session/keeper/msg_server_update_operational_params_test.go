package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestUpdateOperationalParams(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	defaultOp := types.DefaultSessionOperationalParams()

	tests := []struct {
		name        string
		msg         *types.MsgUpdateOperationalParams
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "invalid authority",
			msg: &types.MsgUpdateOperationalParams{
				Authority:         "invalid_address",
				OperationalParams: defaultOp,
			},
			expectError: true,
			errContains: "invalid authority",
		},
		{
			name: "unauthorized address",
			msg: &types.MsgUpdateOperationalParams{
				Authority:         testAddr("random", f.addressCodec),
				OperationalParams: defaultOp,
			},
			expectError: true,
			errContains: "invalid authority",
		},
		{
			name: "success - default operational params",
			msg: &types.MsgUpdateOperationalParams{
				Authority:         authorityStr,
				OperationalParams: defaultOp,
			},
			expectError: false,
		},
		{
			name: "success - reduce active allowlist",
			msg: &types.MsgUpdateOperationalParams{
				Authority: authorityStr,
				OperationalParams: types.SessionOperationalParams{
					AllowedMsgTypes:       types.DefaultAllowedMsgTypes[:3],
					MaxSessionsPerGranter: 10,
					MaxMsgTypesPerSession: 20,
					MaxExpiration:         7 * 24 * time.Hour,
					MaxSpendLimit:         sdk.NewInt64Coin("uspark", 100_000_000),
				},
			},
			expectError: false,
		},
		{
			name: "exceeds ceiling - type not in max_allowed_msg_types",
			msg: &types.MsgUpdateOperationalParams{
				Authority: authorityStr,
				OperationalParams: types.SessionOperationalParams{
					AllowedMsgTypes:       []string{"/sparkdream.unknown.v1.MsgFoo"},
					MaxSessionsPerGranter: 10,
					MaxMsgTypesPerSession: 20,
					MaxExpiration:         7 * 24 * time.Hour,
					MaxSpendLimit:         sdk.NewInt64Coin("uspark", 100_000_000),
				},
			},
			expectError: true,
			errContains: "not in ceiling",
		},
		{
			name: "non-delegable msg type",
			msg: &types.MsgUpdateOperationalParams{
				Authority: authorityStr,
				OperationalParams: types.SessionOperationalParams{
					AllowedMsgTypes:       []string{"/sparkdream.session.v1.MsgExecSession"},
					MaxSessionsPerGranter: 10,
					MaxMsgTypesPerSession: 20,
					MaxExpiration:         7 * 24 * time.Hour,
					MaxSpendLimit:         sdk.NewInt64Coin("uspark", 100_000_000),
				},
			},
			setup: func() {
				// Add to ceiling first to bypass ceiling check
				params, _ := f.keeper.Params.Get(f.ctx)
				params.MaxAllowedMsgTypes = append(params.MaxAllowedMsgTypes, "/sparkdream.session.v1.MsgExecSession")
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "NonDelegableSessionMsgs",
		},
		{
			name: "preserves ceiling",
			msg: &types.MsgUpdateOperationalParams{
				Authority: authorityStr,
				OperationalParams: types.SessionOperationalParams{
					AllowedMsgTypes:       types.DefaultAllowedMsgTypes[:5],
					MaxSessionsPerGranter: 5,
					MaxMsgTypesPerSession: 10,
					MaxExpiration:         3 * 24 * time.Hour,
					MaxSpendLimit:         sdk.NewInt64Coin("uspark", 50_000_000),
				},
			},
			setup: func() {
				_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			_, err := ms.UpdateOperationalParams(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUpdateOperationalParamsPreservesCeiling(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	authorityStr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	// Set initial params
	require.NoError(t, f.keeper.Params.Set(f.ctx, types.DefaultParams()))

	originalCeiling := types.DefaultParams().MaxAllowedMsgTypes

	// Update operational params
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
		Authority: authorityStr,
		OperationalParams: types.SessionOperationalParams{
			AllowedMsgTypes:       types.DefaultAllowedMsgTypes[:3],
			MaxSessionsPerGranter: 5,
			MaxMsgTypesPerSession: 10,
			MaxExpiration:         3 * 24 * time.Hour,
			MaxSpendLimit:         sdk.NewInt64Coin("uspark", 50_000_000),
		},
	})
	require.NoError(t, err)

	// Verify ceiling was preserved
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, originalCeiling, params.MaxAllowedMsgTypes, "ceiling should not change")

	// Verify operational params were applied
	require.Equal(t, types.DefaultAllowedMsgTypes[:3], params.AllowedMsgTypes)
	require.Equal(t, uint64(5), params.MaxSessionsPerGranter)
	require.Equal(t, uint64(10), params.MaxMsgTypesPerSession)
}
