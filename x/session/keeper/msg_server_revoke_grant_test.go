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

func TestRevokeGrant_DirectGranter_SessionKey(t *testing.T) {
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

	rev, err := ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{
		Granter: granter,
		GrantId: resp.GrantId,
	})
	require.NoError(t, err)
	require.True(t, rev.RefundAmount.IsZero())

	_, err = f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.Error(t, err)
}

func TestRevokeGrant_DirectGranter_OneshotTransfer_RefundsDeposit(t *testing.T) {
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

	// Track module-to-account sends (deposit refund path).
	var refunds []sdk.Coins
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, amt sdk.Coins) error {
		refunds = append(refunds, amt)
		return nil
	}

	rev, err := ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{
		Granter: granter,
		GrantId: id,
	})
	require.NoError(t, err)
	require.Equal(t, deposit.String(), rev.RefundAmount.String())
	require.Len(t, refunds, 1)
	require.True(t, refunds[0].Equal(sdk.NewCoins(deposit)))

	// Deposit entry gone.
	_, err = f.keeper.OneshotGasDeposit.Get(f.ctx, id)
	require.Error(t, err)
}

func TestRevokeGrant_UnauthorizedCaller(t *testing.T) {
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

	_, err = ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{
		Granter: stranger,
		GrantId: resp.GrantId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "neither the granter")
}

func TestRevokeGrant_SessionKey_AllowSelfRevoke(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	sessionKeyGrantee := testAddr("session_key", f.addressCodec)
	otherGrantee := testAddr("other_grantee", f.addressCodec)

	// 1) Create a session key WITH allow_self_revoke.
	sessionResp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   sessionKeyGrantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				// allow_self_revoke means MsgRevokeGrant doesn't need to be
				// in allowed_msg_types — the flag is the gate.
				AllowedMsgTypes:  types.DefaultAllowedMsgTypes[:1],
				SpendLimit:       sdk.NewInt64Coin("uspark", 1_000_000),
				MaxExecCount:     10,
				AllowSelfRevoke:  true,
			},
		},
	})
	require.NoError(t, err)
	_ = sessionResp

	// 2) Create a second grant of the SAME granter (a RecurringPull).
	targetResp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   otherGrantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	// 3) Session-key-grantee revokes the target grant.
	rev, err := ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{
		Granter: sessionKeyGrantee, // proto signer == "granter" but msg-server treats this as the caller
		GrantId: targetResp.GrantId,
	})
	require.NoError(t, err)
	require.True(t, rev.RefundAmount.IsZero())

	// Target grant gone.
	_, err = f.keeper.Grants.Get(f.ctx, targetResp.GrantId)
	require.Error(t, err)
}

func TestRevokeGrant_SessionKey_CrossGranterRejected(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granterA := testAddr("granter_a", f.addressCodec)
	granterB := testAddr("granter_b", f.addressCodec)
	sessionKeyGrantee := testAddr("session_key", f.addressCodec)

	// Session key under granterA with allow_self_revoke.
	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granterA,
		Grantee:   sessionKeyGrantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: types.DefaultAllowedMsgTypes[:1],
				SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
				MaxExecCount:    10,
				AllowSelfRevoke: true,
			},
		},
	})
	require.NoError(t, err)

	// Target grant owned by granterB — same grantee address as the
	// session key, different granter.
	targetResp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granterB,
		Grantee:   testAddr("other_grantee", f.addressCodec),
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	// Session-key-grantee tries to revoke a granterB grant — must be rejected.
	_, err = ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{
		Granter: sessionKeyGrantee,
		GrantId: targetResp.GrantId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "neither the granter nor a same-granter session key")
}

func TestRevokeGrant_SessionKey_FlagNotSet_Rejected(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	sessionKeyGrantee := testAddr("session_key", f.addressCodec)
	otherGrantee := testAddr("other_grantee", f.addressCodec)

	// Session key WITHOUT allow_self_revoke.
	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   sessionKeyGrantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: types.DefaultAllowedMsgTypes[:1],
				SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
				MaxExecCount:    10,
				// AllowSelfRevoke intentionally false (default).
			},
		},
	})
	require.NoError(t, err)

	// A target grant.
	targetResp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   otherGrantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	// Session-key-grantee tries to revoke without the flag set.
	_, err = ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{
		Granter: sessionKeyGrantee,
		GrantId: targetResp.GrantId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow_self_revoke")
}

func TestCreateGrant_SessionKey_MsgRevokeGrantInAllowlistRequiresFlag(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// Adding MsgRevokeGrant to allowed_msg_types without allow_self_revoke
	// must be rejected at creation.
	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: []string{
					types.DefaultAllowedMsgTypes[0],
					types.MsgRevokeGrantTypeURL,
				},
				SpendLimit:   sdk.NewInt64Coin("uspark", 1_000_000),
				MaxExecCount: 10,
				// AllowSelfRevoke false (default).
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow_self_revoke")
}

func TestRevokeGrant_TerminalRejected(t *testing.T) {
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

	// Manually flip to a terminal status to exercise the check.
	g, err := f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	g.Status = types.GrantStatus_GRANT_STATUS_COMPLETED
	require.NoError(t, f.keeper.Grants.Set(f.ctx, resp.GrantId, g))

	_, err = ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{
		Granter: granter,
		GrantId: resp.GrantId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "terminal")
}
