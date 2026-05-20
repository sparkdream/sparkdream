package keeper_test

import (
	"context"
	"testing"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// makeTransferOneshot is a small helper that constructs a
// ScheduledOneshotPayload of the Transfer variant.
func makeTransferOneshot(recipient string, amount sdk.Coin, fireAt int64) *types.ScheduledOneshotPayload {
	return &types.ScheduledOneshotPayload{
		FireAt: fireAt,
		Action: &types.ScheduledOneshotPayload_Transfer{
			Transfer: &types.OneshotTransfer{Recipient: recipient, Amount: amount},
		},
	}
}

// createTransferOneshot drives MsgCreateGrant for a Transfer oneshot
// and returns the grant id and the deposit recorded on
// OneshotGasDeposit.
func createTransferOneshot(t *testing.T, f *fixture, ms types.MsgServer, granter, grantee, recipient string, amount sdk.Coin, fireAt int64, expiresAt time.Time) (uint64, sdk.Coin) {
	t.Helper()
	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: expiresAt,
		Payload: &types.MsgCreateGrant_ScheduledOneshot{
			ScheduledOneshot: makeTransferOneshot(recipient, amount, fireAt),
		},
	})
	require.NoError(t, err)
	require.Greater(t, resp.GrantId, uint64(0))
	dep, err := f.keeper.OneshotGasDeposit.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	return resp.GrantId, dep
}

func TestCreateGrant_OneshotTransfer_HappyPath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	// Track deposit movement.
	var moduleSends []sdk.Coins
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(_ context.Context, _ sdk.AccAddress, _ string, amt sdk.Coins) error {
		moduleSends = append(moduleSends, amt)
		return nil
	}

	fireAt := sdkCtx.BlockTime().Add(2 * time.Hour).Unix()
	expiresAt := sdkCtx.BlockTime().Add(24 * time.Hour)

	id, deposit := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 5_000_000),
		fireAt, expiresAt)

	g, err := f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT, g.Type)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)

	// Transfer variant: deposit = max(creation_fee, min_deposit) = 1000.
	require.Equal(t, "1000uspark", deposit.String())
	require.Len(t, moduleSends, 1)
	require.True(t, moduleSends[0].Equal(sdk.NewCoins(sdk.NewInt64Coin("uspark", 1000))))
}

func TestCreateGrant_OneshotTransfer_Validation(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	tests := []struct {
		name        string
		payload     *types.ScheduledOneshotPayload
		expiresAt   time.Time
		errContains string
	}{
		{
			name:        "fire_at too soon",
			payload:     makeTransferOneshot(recipient, sdk.NewInt64Coin("uspark", 1_000_000), sdkCtx.BlockTime().Add(10*time.Second).Unix()),
			expiresAt:   sdkCtx.BlockTime().Add(24 * time.Hour),
			errContains: "fire_at is below",
		},
		{
			name:        "fire_at too far",
			payload:     makeTransferOneshot(recipient, sdk.NewInt64Coin("uspark", 1_000_000), sdkCtx.BlockTime().Add(2*365*24*time.Hour).Unix()),
			expiresAt:   sdkCtx.BlockTime().Add(24 * time.Hour),
			errContains: "exceeds block_time +",
		},
		{
			name:        "fire-vs-expiry buffer too small",
			payload:     makeTransferOneshot(recipient, sdk.NewInt64Coin("uspark", 1_000_000), sdkCtx.BlockTime().Add(23*time.Hour+59*time.Minute).Unix()),
			expiresAt:   sdkCtx.BlockTime().Add(24 * time.Hour), // only 1 minute buffer < 1h default
			errContains: "exceeds expires_at",
		},
		{
			name: "dream denom forbidden",
			payload: &types.ScheduledOneshotPayload{
				FireAt: sdkCtx.BlockTime().Add(2 * time.Hour).Unix(),
				Action: &types.ScheduledOneshotPayload_Transfer{
					Transfer: &types.OneshotTransfer{Recipient: recipient, Amount: sdk.NewInt64Coin("dream", 1_000_000)},
				},
			},
			expiresAt:   sdkCtx.BlockTime().Add(24 * time.Hour),
			errContains: "dream denom is forbidden",
		},
		{
			name: "non-positive amount",
			payload: &types.ScheduledOneshotPayload{
				FireAt: sdkCtx.BlockTime().Add(2 * time.Hour).Unix(),
				Action: &types.ScheduledOneshotPayload_Transfer{
					Transfer: &types.OneshotTransfer{Recipient: recipient, Amount: sdk.NewInt64Coin("uspark", 0)},
				},
			},
			expiresAt:   sdkCtx.BlockTime().Add(24 * time.Hour),
			errContains: "transfer.amount must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
				Granter:   granter,
				Grantee:   grantee,
				ExpiresAt: tt.expiresAt,
				Payload:   &types.MsgCreateGrant_ScheduledOneshot{ScheduledOneshot: tt.payload},
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestCreateGrant_OneshotExec_DepositMath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// Wire a minimal router so the validation router-check passes.
	f.keeper.SetRouter(&fakeRouter{})

	// Use a real allowlisted msg type. Pick a blog msg.
	allowedMsgType := types.DefaultAllowedMsgTypes[0]

	// Build an Any for an empty msg of that type — for now we only need
	// the TypeUrl populated; the inner msg dispatch isn't exercised here.
	innerAny, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{}) // placeholder
	require.NoError(t, err)
	innerAny.TypeUrl = allowedMsgType

	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_ScheduledOneshot{
			ScheduledOneshot: &types.ScheduledOneshotPayload{
				FireAt: sdkCtx.BlockTime().Add(2 * time.Hour).Unix(),
				Action: &types.ScheduledOneshotPayload_Exec{
					Exec: &types.OneshotExec{
						Msg:      innerAny,
						GasLimit: 100_000,
					},
				},
			},
		},
	})
	require.NoError(t, err)

	dep, err := f.keeper.OneshotGasDeposit.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	// Exec deposit = ceil(100000 * 0.0025) + 1000 = 250 + 1000 = 1250.
	require.Equal(t, "1250uspark", dep.String())
}

func TestCreateGrant_OneshotExec_DenyRecursion(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.keeper.SetRouter(&fakeRouter{})

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// Inner msg type is in NonDelegableSessionMsgs.
	innerAny, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{})
	require.NoError(t, err)
	innerAny.TypeUrl = "/sparkdream.session.v1.MsgCreateGrant"

	_, err = ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_ScheduledOneshot{
			ScheduledOneshot: &types.ScheduledOneshotPayload{
				FireAt: sdkCtx.BlockTime().Add(2 * time.Hour).Unix(),
				Action: &types.ScheduledOneshotPayload_Exec{
					Exec: &types.OneshotExec{Msg: innerAny, GasLimit: 100_000},
				},
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "NonDelegableSessionMsgs")
}

func TestCreateGrant_OneshotExec_GasOutOfRange(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.keeper.SetRouter(&fakeRouter{})

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	innerAny, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{})
	require.NoError(t, err)
	innerAny.TypeUrl = types.DefaultAllowedMsgTypes[0]

	_, err = ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_ScheduledOneshot{
			ScheduledOneshot: &types.ScheduledOneshotPayload{
				FireAt: sdkCtx.BlockTime().Add(2 * time.Hour).Unix(),
				Action: &types.ScheduledOneshotPayload_Exec{
					Exec: &types.OneshotExec{Msg: innerAny, GasLimit: 10},
				},
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "gas_limit")
}

func TestCreateGrant_OneshotExec_RouterUnwired(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	// Intentionally do NOT wire router.

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	innerAny, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{})
	require.NoError(t, err)
	innerAny.TypeUrl = types.DefaultAllowedMsgTypes[0]

	_, err = ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_ScheduledOneshot{
			ScheduledOneshot: &types.ScheduledOneshotPayload{
				FireAt: sdkCtx.BlockTime().Add(2 * time.Hour).Unix(),
				Action: &types.ScheduledOneshotPayload_Exec{
					Exec: &types.OneshotExec{Msg: innerAny, GasLimit: 100_000},
				},
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "msg router is not wired")
}

func TestFire_Transfer_HappyPath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	fireAt := sdkCtx.BlockTime().Add(2 * time.Hour).Unix()
	expiresAt := sdkCtx.BlockTime().Add(24 * time.Hour)
	id, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 5_000_000), fireAt, expiresAt)

	// Track bank sends from the fire path (transfer + deposit refund).
	var sends []sdk.Coins
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, amt sdk.Coins) error {
		sends = append(sends, amt)
		return nil
	}
	var moduleToModule []sdk.Coins
	f.bankKeeper.SendCoinsFromModuleToModuleFn = func(_ context.Context, _, _ string, amt sdk.Coins) error {
		moduleToModule = append(moduleToModule, amt)
		return nil
	}

	// Fire pass: advance block time past fire_at.
	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(3*time.Hour))
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(futureCtx)))

	g, err := f.keeper.Grants.Get(futureCtx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_FIRED, g.Status)

	// Transfer was attempted.
	require.GreaterOrEqual(t, len(sends), 1)
	require.True(t, sends[0].Equal(sdk.NewCoins(sdk.NewInt64Coin("uspark", 5_000_000))))
	// Deposit moved to fee collector.
	require.GreaterOrEqual(t, len(moduleToModule), 1)
}

func TestFire_Transfer_HandlerErrorCaptured(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	fireAt := sdkCtx.BlockTime().Add(2 * time.Hour).Unix()
	expiresAt := sdkCtx.BlockTime().Add(24 * time.Hour)
	id, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 5_000_000), fireAt, expiresAt)

	// Bank send fails — fire path should capture the error and still
	// transition to FIRED (granter authorized one attempt).
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
		return testErr("insufficient funds at fire time")
	}

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(3*time.Hour))
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(futureCtx)))

	g, err := f.keeper.Grants.Get(futureCtx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_FIRED, g.Status)
	so := g.GetScheduledOneshot()
	require.NotNil(t, so)
	require.Contains(t, so.FireError, "insufficient")
}

func TestRetryScheduledOneshot_TypeMismatch(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// Create a RecurringPull grant.
	rpResp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
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

	_, err = ms.RetryScheduledOneshot(f.ctx, &types.MsgRetryScheduledOneshot{
		Caller:  granter,
		GrantId: rpResp.GrantId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected SCHEDULED_ONESHOT")
}

func TestRetryScheduledOneshot_NotPaused(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 1_000_000),
		sdkCtx.BlockTime().Add(2*time.Hour).Unix(),
		sdkCtx.BlockTime().Add(24*time.Hour))

	// Grant is still ACTIVE — retry should reject with NotPaused.
	_, err := ms.RetryScheduledOneshot(f.ctx, &types.MsgRetryScheduledOneshot{
		Caller:  granter,
		GrantId: id,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "is ACTIVE")
}

func TestRetryScheduledOneshot_Unauthorized(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)
	stranger := testAddr("stranger", f.addressCodec)

	id, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 1_000_000),
		sdkCtx.BlockTime().Add(2*time.Hour).Unix(),
		sdkCtx.BlockTime().Add(24*time.Hour))

	// Manually transition to PAUSED so the auth check is the only gate.
	g, _ := f.keeper.Grants.Get(f.ctx, id)
	g.Status = types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS
	require.NoError(t, f.keeper.Grants.Set(f.ctx, id, g))

	_, err := ms.RetryScheduledOneshot(f.ctx, &types.MsgRetryScheduledOneshot{
		Caller:  stranger,
		GrantId: id,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "neither granter")
}

// fakeRouter is a minimal MessageRouter stub used by tests that need
// the router non-nil for the OneshotExec creation gate. It returns nil
// for Handler — actual handler dispatch isn't exercised by the
// validation-focused tests.
type fakeRouter struct{}

func (fakeRouter) Handler(sdk.Msg) baseAppMsgServiceHandler { return nil }
func (fakeRouter) HandlerByTypeURL(string) baseAppMsgServiceHandler { return nil }

// baseAppMsgServiceHandler shadows the cosmos-sdk baseapp.MsgServiceHandler
// type alias so the test file can declare the fake without importing
// baseapp directly (which would pull a lot into the test surface).
type baseAppMsgServiceHandler = func(ctx sdk.Context, msg sdk.Msg) (*sdk.Result, error)

// testErr is a small error helper to avoid pulling errors.New into the
// list of imports.
type stringErr string

func (e stringErr) Error() string { return string(e) }
func testErr(s string) error      { return stringErr(s) }
