package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
	sessiontypes "sparkdream/x/session/types"
)

// ---------------------------------------------------------------------------
// Wrapper-layer tests for M5–M8. These verify the translation layer
// (inputs → session payload, session errors → commons errors). Session
// claim mechanics — period clock, status transitions, hook PreCheck /
// PostCommit — are exercised in x/session/keeper/*_test.go (M1+M2) and
// x/commons/keeper/session_claim_hook_test.go (M4); the wrapper tests
// trust those.
// ---------------------------------------------------------------------------

// scheduleFixture wires a commons keeper + mockSessionKeeper + a
// registered council with the given max_spend_per_epoch. Returns the
// msgServer, the council address, the mock, and the active SDK
// context.
type scheduleFixture struct {
	t       *testing.T
	k       keeper.Keeper
	ctx     sdk.Context
	mock    *mockSessionKeeper
	msrv    types.MsgServer
	council sdk.AccAddress
}

func setupWrapperFixture(t *testing.T, councilMaxSpend *math.Int) *scheduleFixture {
	t.Helper()
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	mock := newMockSessionKeeper()
	k.SetSessionKeeper(mock)

	council := sdk.AccAddress([]byte("wrapper_council_____"))
	now := ctx.BlockTime().Unix()
	registerCouncil(t, k, ctx, "Wrap", council, types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      councilMaxSpend,
		CurrentTermExpiration: now + 30*24*3600,
	})

	return &scheduleFixture{
		t:       t,
		k:       k,
		ctx:     ctx,
		mock:    mock,
		msrv:    keeper.NewMsgServerImpl(k),
		council: council,
	}
}

func (f *scheduleFixture) recipient() string {
	return sdk.AccAddress([]byte("wrapper_recipient___")).String()
}

func (f *scheduleFixture) commonsModuleAddr() string {
	return authtypes.NewModuleAddress(types.ModuleName).String()
}

// ---------------------------------------------------------------------------
// M5 — ScheduleRecurringSpend wrapper tests.
// ---------------------------------------------------------------------------

func TestScheduleWrapper_HappyPath_RoutesToSession(t *testing.T) {
	limit := math.NewInt(100_000)
	f := setupWrapperFixture(t, &limit)

	endTime := f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix()
	resp, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000))),
		PeriodSeconds:   86_400,
		EndTime:         endTime,
		Note:            "monthly grant",
	})
	require.NoError(t, err)
	require.NotZero(t, resp.Id)

	// Exactly one CreateGrantOnBehalfOf call with the right caller.
	require.Len(t, f.mock.CreateCalls, 1)
	call := f.mock.CreateCalls[0]
	require.Equal(t, f.commonsModuleAddr(), call.CallerModule)
	require.Equal(t, f.council.String(), call.Msg.Granter)
	require.Equal(t, f.recipient(), call.Msg.Grantee)
	require.Equal(t, time.Unix(endTime, 0).UTC(), call.Msg.ExpiresAt)
	require.Equal(t, "monthly grant", call.Msg.Note)

	// Payload was the RecurringPull variant with the expected fields.
	rp := call.Msg.GetRecurringPull()
	require.NotNil(t, rp)
	require.Equal(t, "uspark", rp.AmountPerPeriod.Denom)
	require.Equal(t, int64(1_000), rp.AmountPerPeriod.Amount.Int64())
	require.Equal(t, int64(86_400), rp.PeriodSeconds)
}

func TestScheduleWrapper_InvalidAuthorityAddress(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       "not-a-bech32-address",
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000))),
		PeriodSeconds:   86_400,
		EndTime:         f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority address")
	require.Empty(t, f.mock.CreateCalls)
}

func TestScheduleWrapper_AuthorityNotAGroupPolicy(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	stranger := sdk.AccAddress([]byte("not_a_council_______")).String()
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       stranger,
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000))),
		PeriodSeconds:   86_400,
		EndTime:         f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
	})
	require.ErrorIs(t, err, types.ErrGroupNotFound)
	require.Empty(t, f.mock.CreateCalls)
}

func TestScheduleWrapper_MultiCoinRejected_D1a(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority: f.council.String(),
		Recipient: f.recipient(),
		AmountPerPeriod: sdk.NewCoins(
			sdk.NewCoin("uspark", math.NewInt(1_000)),
			sdk.NewCoin("uatom", math.NewInt(500)),
		),
		PeriodSeconds: 86_400,
		EndTime:       f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exactly one coin")
	require.Empty(t, f.mock.CreateCalls)
}

func TestScheduleWrapper_ZeroAmountRejected(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(),
		PeriodSeconds:   86_400,
		EndTime:         f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "amount_per_period")
}

func TestScheduleWrapper_NoteTooLongRejected(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	longNote := make([]byte, keeper.MaxRecurringSpendNoteLen+1)
	for i := range longNote {
		longNote[i] = 'x'
	}
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000))),
		PeriodSeconds:   86_400,
		EndTime:         f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
		Note:            string(longNote),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "note exceeds")
}

func TestScheduleWrapper_StartTimeInPastRejected(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	pastStart := f.ctx.BlockTime().Add(-1 * time.Hour).Unix()
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000))),
		PeriodSeconds:   86_400,
		StartTime:       pastStart,
		EndTime:         f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInvalidWindow)
}

func TestScheduleWrapper_EndTimeBeforeStartRejected(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000))),
		PeriodSeconds:   86_400,
		StartTime:       f.ctx.BlockTime().Add(10 * 24 * time.Hour).Unix(),
		EndTime:         f.ctx.BlockTime().Add(5 * 24 * time.Hour).Unix(),
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInvalidWindow)
}

func TestScheduleWrapper_WindowShorterThanPeriodRejected(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	start := f.ctx.BlockTime().Unix()
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000))),
		PeriodSeconds:   86_400 * 7, // 1 week period
		StartTime:       start,
		EndTime:         start + 86_400*3, // 3-day window
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInvalidWindow)
}

// TestScheduleWrapper_MaxPerEpoch_NoCouncilCap confirms the nil-safe
// formula: when MaxSpendPerEpoch is nil, the per-grant cap is
// 10 × amount_per_period.
func TestScheduleWrapper_MaxPerEpoch_NoCouncilCap(t *testing.T) {
	f := setupWrapperFixture(t, nil) // no council cap
	amt := math.NewInt(1_000)
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", amt)),
		PeriodSeconds:   86_400,
		EndTime:         f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
	})
	require.NoError(t, err)
	require.Len(t, f.mock.CreateCalls, 1)
	rp := f.mock.CreateCalls[0].Msg.GetRecurringPull()
	expected := amt.MulRaw(10).String()
	require.Equal(t, expected, rp.MaxPerEpoch,
		"with no council cap, per-grant cap = 10 × amount_per_period")
}

// TestScheduleWrapper_MaxPerEpoch_CouncilCapDominates verifies when
// council cap > 10 × amount, the council cap is used.
func TestScheduleWrapper_MaxPerEpoch_CouncilCapDominates(t *testing.T) {
	councilCap := math.NewInt(100_000) // > 10 × 1_000
	f := setupWrapperFixture(t, &councilCap)
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000))),
		PeriodSeconds:   86_400,
		EndTime:         f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
	})
	require.NoError(t, err)
	rp := f.mock.CreateCalls[0].Msg.GetRecurringPull()
	require.Equal(t, councilCap.String(), rp.MaxPerEpoch,
		"council cap dominates when > 10 × amount")
}

// TestScheduleWrapper_MaxPerEpoch_DefaultDominatesSmallCap verifies
// when council cap < 10 × amount, the 10 × amount default is used.
func TestScheduleWrapper_MaxPerEpoch_DefaultDominatesSmallCap(t *testing.T) {
	councilCap := math.NewInt(500) // < 10 × 1_000
	f := setupWrapperFixture(t, &councilCap)
	amt := math.NewInt(1_000)
	_, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", amt)),
		PeriodSeconds:   86_400,
		EndTime:         f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
	})
	require.NoError(t, err)
	rp := f.mock.CreateCalls[0].Msg.GetRecurringPull()
	require.Equal(t, amt.MulRaw(10).String(), rp.MaxPerEpoch,
		"10 × amount dominates when council cap is smaller")
}

// ---------------------------------------------------------------------------
// M6 — CancelRecurringSpend wrapper tests.
// ---------------------------------------------------------------------------

func TestCancelWrapper_HappyPath(t *testing.T) {
	limit := math.NewInt(100_000)
	f := setupWrapperFixture(t, &limit)
	id := scheduleViaWrapper(t, f)

	_, err := f.msrv.CancelRecurringSpend(f.ctx, &types.MsgCancelRecurringSpend{
		Authority: f.council.String(),
		Id:        id,
	})
	require.NoError(t, err)

	require.Len(t, f.mock.RevokeCalls, 1)
	require.Equal(t, f.commonsModuleAddr(), f.mock.RevokeCalls[0].CallerModule)
	require.Equal(t, id, f.mock.RevokeCalls[0].GrantID)
	require.True(t, f.mock.deleted[id])
}

func TestCancelWrapper_GrantNotFoundTranslatesToInactive(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	_, err := f.msrv.CancelRecurringSpend(f.ctx, &types.MsgCancelRecurringSpend{
		Authority: f.council.String(),
		Id:        9999,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInactive,
		"NotFound from session must translate to ErrRecurringSpendInactive (L7)")
	require.Empty(t, f.mock.RevokeCalls,
		"wrapper short-circuits before calling RevokeGrantInternal on NotFound")
}

func TestCancelWrapper_AuthorityMismatch(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	id := scheduleViaWrapper(t, f)
	imposter := sdk.AccAddress([]byte("imposter_council____")).String()

	_, err := f.msrv.CancelRecurringSpend(f.ctx, &types.MsgCancelRecurringSpend{
		Authority: imposter,
		Id:        id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendUnauthorized)
	require.Empty(t, f.mock.RevokeCalls,
		"wrapper short-circuits before calling RevokeGrantInternal on authority mismatch")
}

func TestCancelWrapper_TerminalGrantTranslatesToInactive(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	id := scheduleViaWrapper(t, f)

	// Inject the ErrGrantTerminal path.
	f.mock.RevokeGrantInternalFn = func(_ context.Context, _ string, _ uint64) (sdk.Coin, error) {
		return sdk.Coin{}, sessiontypes.ErrGrantTerminal.Wrap("test")
	}

	_, err := f.msrv.CancelRecurringSpend(f.ctx, &types.MsgCancelRecurringSpend{
		Authority: f.council.String(),
		Id:        id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInactive,
		"ErrGrantTerminal from session must translate to ErrRecurringSpendInactive (L7)")
}

// ---------------------------------------------------------------------------
// M7 — DeclineRecurringSpend wrapper tests.
// ---------------------------------------------------------------------------

func TestDeclineWrapper_HappyPath(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	id := scheduleViaWrapper(t, f)

	_, err := f.msrv.DeclineRecurringSpend(f.ctx, &types.MsgDeclineRecurringSpend{
		Recipient: f.recipient(),
		Id:        id,
	})
	require.NoError(t, err)
	require.Len(t, f.mock.DeclineCalls, 1)
	require.Equal(t, f.commonsModuleAddr(), f.mock.DeclineCalls[0].CallerModule)
	require.Equal(t, id, f.mock.DeclineCalls[0].GrantID)
	require.Equal(t, f.recipient(), f.mock.DeclineCalls[0].Grantee)
	require.True(t, f.mock.deleted[id])
}

func TestDeclineWrapper_GrantNotFoundTranslatesToInactive(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	_, err := f.msrv.DeclineRecurringSpend(f.ctx, &types.MsgDeclineRecurringSpend{
		Recipient: f.recipient(),
		Id:        9999,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInactive)
	require.Empty(t, f.mock.DeclineCalls)
}

func TestDeclineWrapper_RecipientMismatch(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	id := scheduleViaWrapper(t, f)
	imposter := sdk.AccAddress([]byte("imposter_recipient__")).String()

	_, err := f.msrv.DeclineRecurringSpend(f.ctx, &types.MsgDeclineRecurringSpend{
		Recipient: imposter,
		Id:        id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendUnauthorized)
	require.Empty(t, f.mock.DeclineCalls)
}

func TestDeclineWrapper_TerminalGrantTranslatesToInactive(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	id := scheduleViaWrapper(t, f)

	f.mock.DeclineGrantInternalFn = func(_ context.Context, _ string, _ uint64, _ string) (sdk.Coin, error) {
		return sdk.Coin{}, sessiontypes.ErrGrantTerminal.Wrap("test")
	}

	_, err := f.msrv.DeclineRecurringSpend(f.ctx, &types.MsgDeclineRecurringSpend{
		Recipient: f.recipient(),
		Id:        id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInactive)
}

// ---------------------------------------------------------------------------
// M8 — ClaimRecurringSpend wrapper tests.
// ---------------------------------------------------------------------------

func TestClaimWrapper_HappyPath(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	id := scheduleViaWrapper(t, f)

	// Stub the claim to return a canned response.
	f.mock.ClaimRecurringPullForGranteeF = func(_ context.Context, _ string, gid uint64, gtee string) (*sessiontypes.MsgClaimRecurringPullResponse, error) {
		require.Equal(t, id, gid)
		require.Equal(t, f.recipient(), gtee)
		return &sessiontypes.MsgClaimRecurringPullResponse{
			ClaimNumber:   3,
			NextClaimTime: 12345,
		}, nil
	}

	resp, err := f.msrv.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: f.recipient(),
		Id:        id,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(3), resp.ClaimNumber)
	require.Equal(t, int64(12345), resp.NextClaimTime)

	require.Len(t, f.mock.ClaimCalls, 1)
	require.Equal(t, f.commonsModuleAddr(), f.mock.ClaimCalls[0].CallerModule)
}

func TestClaimWrapper_InvalidRecipientAddress(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	_, err := f.msrv.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: "not-a-bech32-address",
		Id:        1,
	})
	require.Error(t, err)
	require.Empty(t, f.mock.ClaimCalls)
}

func TestClaimWrapper_SessionErrorBubblesUp(t *testing.T) {
	f := setupWrapperFixture(t, nil)
	// Inject a generic session-side error and verify it propagates.
	f.mock.ClaimRecurringPullForGranteeF = func(_ context.Context, _ string, _ uint64, _ string) (*sessiontypes.MsgClaimRecurringPullResponse, error) {
		return nil, sessiontypes.ErrRecurringPullNotDue.Wrap("not yet eligible")
	}
	_, err := f.msrv.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: f.recipient(),
		Id:        1,
	})
	require.ErrorIs(t, err, sessiontypes.ErrRecurringPullNotDue)
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

func scheduleViaWrapper(t *testing.T, f *scheduleFixture) uint64 {
	t.Helper()
	resp, err := f.msrv.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000))),
		PeriodSeconds:   86_400,
		EndTime:         f.ctx.BlockTime().Add(30 * 24 * time.Hour).Unix(),
	})
	require.NoError(t, err)
	return resp.Id
}
