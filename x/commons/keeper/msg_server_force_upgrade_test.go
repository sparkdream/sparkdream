package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func TestMsgForceUpgrade(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	// 1. Setup Addresses
	addrBytes := []byte("policyAddr________________")
	policyAddr, err := f.addressCodec.BytesToString(addrBytes)
	require.NoError(t, err)

	unauthBytes := []byte("unauthorizedAddr__________")
	unauthAddr, err := f.addressCodec.BytesToString(unauthBytes)
	require.NoError(t, err)

	// 2. Setup Permissions
	// Explicitly grant 'MsgForceUpgrade' permission to policyAddr
	msgType := sdk.MsgTypeURL(&types.MsgForceUpgrade{})
	err = f.keeper.PolicyPermissions.Set(f.ctx, policyAddr, types.PolicyPermissions{
		PolicyAddress:   policyAddr,
		AllowedMessages: []string{msgType},
	})
	require.NoError(t, err)

	// 3. Define Plans
	validPlan := types.UpgradePlan{
		Name:   "v2-upgrade",
		Height: 1000,
		Info:   "standard upgrade",
	}

	failPlan := types.UpgradePlan{
		Name:   "fail", // Triggers error in mockUpgradeKeeper (see keeper_test.go)
		Height: 1000,
	}

	tests := []struct {
		name      string
		msg       *types.MsgForceUpgrade
		expectErr bool
		errMsg    string
	}{
		{
			name: "success - authorized policy schedules upgrade",
			msg: &types.MsgForceUpgrade{
				Authority: policyAddr,
				Plan:      validPlan,
			},
			expectErr: false,
		},
		{
			name: "failure - unauthorized signer (no permissions set)",
			msg: &types.MsgForceUpgrade{
				Authority: unauthAddr,
				Plan:      validPlan,
			},
			expectErr: true,
			errMsg:    "signer has no permissions",
		},
		{
			name: "failure - authorized policy but upgrade keeper fails",
			msg: &types.MsgForceUpgrade{
				Authority: policyAddr,
				Plan:      failPlan,
			},
			expectErr: true,
			errMsg:    "invalid request", // From mockUpgradeKeeper
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.ForceUpgrade(f.ctx, tc.msg)
			if tc.expectErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					require.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
