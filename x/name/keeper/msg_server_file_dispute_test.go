package keeper_test

import (
	"testing"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"

	commonstypes "sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

// Mock External Types from x/commons
const CommonsCouncilName = "Commons Council"

var (
	// Valid addresses for testing
	ClaimantAddr      = sdk.AccAddress([]byte("claimant_address____")).String()
	CouncilPolicyAddr = sdk.AccAddress([]byte("council_policy_addr_"))
	DisputeFee        = sdk.NewCoin("uspark", math.NewInt(1000))
)

func TestFileDispute(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	tests := []struct {
		name          string
		msg           *types.MsgFileDispute
		setup         func()
		expectPass    bool
		expectErrCode *errorsmod.Error
	}{
		{
			name: "Success - Transfer Fee to Council",
			msg: &types.MsgFileDispute{
				Authority: ClaimantAddr,
				Name:      "alice",
			},
			setup: func() {
				// 1. Reset Mock state via fixture
				f.mockBank.Reset()
				f.mockCommons.Reset()

				// 2. Setup specific state
				f.keeper.SetParams(f.ctx, types.Params{DisputeFee: DisputeFee})
				f.mockCommons.ExtendedGroups[CommonsCouncilName] = commonstypes.ExtendedGroup{
					PolicyAddress: CouncilPolicyAddr.String(),
				}
			},
			expectPass: true,
		},
		{
			name: "Success - Zero Fee (Transfer Skipped)",
			msg: &types.MsgFileDispute{
				Authority: ClaimantAddr,
				Name:      "bob",
			},
			setup: func() {
				f.mockBank.Reset()
				f.mockCommons.Reset()

				f.keeper.SetParams(f.ctx, types.Params{DisputeFee: sdk.NewCoin(DisputeFee.Denom, math.ZeroInt())})
				f.mockCommons.ExtendedGroups[CommonsCouncilName] = commonstypes.ExtendedGroup{
					PolicyAddress: CouncilPolicyAddr.String(),
				}
			},
			expectPass: true,
		},
		{
			name: "Failure - Insufficient Funds (Bank Error)",
			msg: &types.MsgFileDispute{
				Authority: ClaimantAddr,
				Name:      "carol",
			},
			setup: func() {
				f.mockBank.Reset()
				f.mockCommons.Reset()

				f.keeper.SetParams(f.ctx, types.Params{DisputeFee: DisputeFee})
				f.mockCommons.ExtendedGroups[CommonsCouncilName] = commonstypes.ExtendedGroup{
					PolicyAddress: CouncilPolicyAddr.String(),
				}
				f.mockBank.sendErr = sdkerrors.ErrInsufficientFunds
			},
			expectPass:    false,
			expectErrCode: sdkerrors.ErrInsufficientFunds,
		},
		{
			name: "Failure - Council Group Not Found",
			msg: &types.MsgFileDispute{
				Authority: ClaimantAddr,
				Name:      "dave",
			},
			setup: func() {
				f.mockBank.Reset()
				f.mockCommons.Reset()
				f.keeper.SetParams(f.ctx, types.Params{DisputeFee: DisputeFee})
				// Intentionally do NOT add the group to mockCK
			},
			expectPass:    false,
			expectErrCode: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "Failure - Invalid Policy Address Format",
			msg: &types.MsgFileDispute{
				Authority: ClaimantAddr,
				Name:      "eve",
			},
			setup: func() {
				f.mockBank.Reset()
				f.mockCommons.Reset()
				f.keeper.SetParams(f.ctx, types.Params{DisputeFee: DisputeFee})

				f.mockCommons.ExtendedGroups[CommonsCouncilName] = commonstypes.ExtendedGroup{
					PolicyAddress: "invalid_bech32_address",
				}
			},
			expectPass: false,
			// Use the sentinel error directly.
			// The loop logic below specifically checks for the "invalid policy address..." string
			// when this specific error code is present.
			expectErrCode: sdkerrors.ErrInvalidAddress,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()

			// Use cache context to prevent KVStore state leaking between tests
			cacheCtx, _ := f.ctx.CacheContext()

			_, err := ms.FileDispute(cacheCtx, tc.msg)

			if tc.expectPass {
				require.NoError(t, err)

				// Verify fee transfer check
				currentParams := f.keeper.GetParams(cacheCtx)
				if !currentParams.DisputeFee.IsZero() {
					transferKey := ClaimantAddr + "|" + CouncilPolicyAddr.String()

					coins, found := f.mockBank.SentCoins[transferKey]
					require.True(t, found, "Expected fee transfer to council policy address")
					require.True(t, coins.Equal(sdk.NewCoins(currentParams.DisputeFee)), "Fee amount mismatch")
				}

			} else {
				require.Error(t, err)
				if tc.expectErrCode != nil {
					// This logic allows using *errorsmod.Error for the Is() check
					if tc.expectErrCode.Is(sdkerrors.ErrInvalidAddress) {
						require.Contains(t, err.Error(), "invalid policy address stored for Commons Council")
					} else {
						require.ErrorIs(t, err, tc.expectErrCode)
					}
				}
			}
		})
	}
}
