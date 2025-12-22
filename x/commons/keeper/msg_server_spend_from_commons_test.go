package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// --- Test Suite ---

func TestSpendFromCommons(t *testing.T) {
	k, ctx, mockBK := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)

	// 1. Setup Addresses
	councilAddr := sdk.AccAddress([]byte("council_address_____"))
	recipientAddr := sdk.AccAddress([]byte("recipient_address___"))
	attackerAddr := sdk.AccAddress([]byte("attacker_address____"))
	shellAddr := sdk.AccAddress([]byte("shell_address_______"))
	zombieAddr := sdk.AccAddress([]byte("zombie_address______"))

	// 2. Fund the Authorities (Mock Bank)
	fundAmount := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(10000)))
	mockBK.balance[councilAddr.String()] = fundAmount
	mockBK.balance[shellAddr.String()] = fundAmount
	mockBK.balance[zombieAddr.String()] = fundAmount

	// 3. Register Groups in KVStore
	now := ctx.BlockTime().Unix()

	// A. Valid Council (Active, 1000 limit)
	validGroup := types.ExtendedGroup{
		PolicyAddress:         councilAddr.String(),
		MaxSpendPerEpoch:      "1000uspark",
		ActivationTime:        0,
		CurrentTermExpiration: now + 3600, // Valid for 1 hour
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "ValidCouncil", validGroup))
	// NEW: Must set Index for O(1) lookup
	require.NoError(t, k.PolicyToName.Set(ctx, councilAddr.String(), "ValidCouncil"))

	// B. Shell Group (Future Activation)
	shellGroup := types.ExtendedGroup{
		PolicyAddress:  shellAddr.String(),
		ActivationTime: now + 3600, // Active in 1 hour
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "ShellGroup", shellGroup))
	// NEW: Must set Index
	require.NoError(t, k.PolicyToName.Set(ctx, shellAddr.String(), "ShellGroup"))

	// C. Zombie Group (Expired Term)
	zombieGroup := types.ExtendedGroup{
		PolicyAddress:         zombieAddr.String(),
		CurrentTermExpiration: now - 3600, // Expired 1 hour ago
	}
	require.NoError(t, k.ExtendedGroup.Set(ctx, "ZombieGroup", zombieGroup))
	// NEW: Must set Index
	require.NoError(t, k.PolicyToName.Set(ctx, zombieAddr.String(), "ZombieGroup"))

	// 4. Run Test Cases
	tests := []struct {
		desc    string
		msg     *types.MsgSpendFromCommons
		check   func(t *testing.T)
		err     error
		errCode codes.Code
	}{
		{
			desc: "Success - Valid Spend by Council",
			msg: &types.MsgSpendFromCommons{
				Authority: councilAddr.String(),
				Recipient: recipientAddr.String(),
				Amount:    sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
			},
			check: func(t *testing.T) {
				bal := mockBK.balance[recipientAddr.String()]
				require.Equal(t, math.NewInt(100), bal.AmountOf("uspark"))
			},
		},
		{
			desc: "Failure - Not Registered (Signer is not a Group)",
			msg: &types.MsgSpendFromCommons{
				Authority: attackerAddr.String(),
				Recipient: recipientAddr.String(),
				Amount:    sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
			},
			err: types.ErrGroupNotFound,
		},
		{
			desc: "Failure - Shell Group (Pre-launch Lock)",
			msg: &types.MsgSpendFromCommons{
				Authority: shellAddr.String(),
				Recipient: recipientAddr.String(),
				Amount:    sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
			},
			err: types.ErrGroupNotActive,
		},
		{
			desc: "Failure - Zombie Group (Term Expired)",
			msg: &types.MsgSpendFromCommons{
				Authority: zombieAddr.String(),
				Recipient: recipientAddr.String(),
				Amount:    sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
			},
			err: types.ErrGroupExpired,
		},
		{
			desc: "Failure - Rate Limit Exceeded (Over Cap)",
			msg: &types.MsgSpendFromCommons{
				Authority: councilAddr.String(), // Limit is 1000
				Recipient: recipientAddr.String(),
				Amount:    sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(2000))),
			},
			err: types.ErrRateLimitExceeded,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			// Clear recipient balance before each run to avoid accumulation logic issues in check
			mockBK.balance[recipientAddr.String()] = sdk.NewCoins()

			// Reset time for each run to ensure consistency
			ctx = ctx.WithBlockTime(time.Unix(now, 0))

			_, err := ms.SpendFromCommons(ctx, tc.msg)

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else if tc.errCode != codes.OK {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tc.check != nil {
					tc.check(t)
				}
			}
		})
	}
}
