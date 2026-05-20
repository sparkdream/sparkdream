package keeper_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestDeclineGrant_SessionKey(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: types.DefaultAllowedMsgTypes[:1],
				SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
				MaxExecCount:    10,
			},
		},
	})
	require.NoError(t, err)

	declResp, err := ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{
		Grantee: grantee,
		GrantId: resp.GrantId,
	})
	require.NoError(t, err)
	require.True(t, declResp.RefundAmount.IsZero())

	_, err = f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.Error(t, err)

	// Active grant counter for session-keys back to zero.
	count, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
	require.NoError(t, err)
	require.Equal(t, uint32(0), count)
}

func TestDeclineGrant_RecurringPull(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	_, err = ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{
		Grantee: grantee,
		GrantId: resp.GrantId,
	})
	require.NoError(t, err)

	// Subsequent claim attempt should fail (grant gone).
	_, err = ms.ClaimRecurringPull(f.ctx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: resp.GrantId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestDeclineGrant_Oneshot_RefundsDeposit(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id, deposit := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 1_000_000),
		sdkCtx.BlockTime().Add(2*time.Hour).Unix(),
		sdkCtx.BlockTime().Add(24*time.Hour))

	var refunds []sdk.Coins
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, amt sdk.Coins) error {
		refunds = append(refunds, amt)
		return nil
	}

	declResp, err := ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{
		Grantee: grantee,
		GrantId: id,
	})
	require.NoError(t, err)
	require.Equal(t, deposit.String(), declResp.RefundAmount.String())
	require.Len(t, refunds, 1)
	require.True(t, refunds[0].Equal(sdk.NewCoins(deposit)))

	// Deposit entry gone.
	_, err = f.keeper.OneshotGasDeposit.Get(f.ctx, id)
	require.Error(t, err)

	// EndBlocker fire pass should NOT fire this grant (it's gone).
	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(3*time.Hour))
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(futureCtx)))
}

func TestDeclineGrant_UnauthorizedCaller(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	stranger := testAddr("stranger", f.addressCodec)

	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: types.DefaultAllowedMsgTypes[:1],
				SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
				MaxExecCount:    10,
			},
		},
	})
	require.NoError(t, err)

	_, err = ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{
		Grantee: stranger,
		GrantId: resp.GrantId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not the grantee")
}

func TestDeclineGrant_TerminalRejected(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: types.DefaultAllowedMsgTypes[:1],
				SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
				MaxExecCount:    10,
			},
		},
	})
	require.NoError(t, err)

	// Decline first.
	_, err = ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{
		Grantee: grantee,
		GrantId: resp.GrantId,
	})
	require.NoError(t, err)

	// Second decline must fail (grant gone).
	_, err = ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{
		Grantee: grantee,
		GrantId: resp.GrantId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}
