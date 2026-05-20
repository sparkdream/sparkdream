package keeper_test

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// recordingHook captures every (grant, amount) it sees on PreCheck
// AND PostCommit, with separate call lists per phase. Optional
// `vetoOnAmount` returns an error from PreCheck when the call's
// amount equals it, letting tests verify veto-and-short-circuit
// semantics. `failPostCommitOnAmount` returns an error from
// PostCommit when the amount matches, exercising the
// PostCommit-halts-tx contract.
type recordingHook struct {
	name                   string
	preCheckCalls          []hookCall
	postCommitCalls        []hookCall
	vetoOnAmount           sdk.Coins
	failPostCommitOnAmount sdk.Coins
}

type hookCall struct {
	GrantID uint64
	Type    types.GrantType
	Granter string
	Amount  sdk.Coins
}

func (h *recordingHook) PreCheck(_ context.Context, grant types.Grant, amount sdk.Coins) error {
	h.preCheckCalls = append(h.preCheckCalls, hookCall{
		GrantID: grant.Id,
		Type:    grant.Type,
		Granter: grant.Granter,
		Amount:  amount,
	})
	if h.vetoOnAmount != nil && amount.Equal(h.vetoOnAmount) {
		return errors.New("vetoed by " + h.name)
	}
	return nil
}

func (h *recordingHook) PostCommit(_ context.Context, grant types.Grant, amount sdk.Coins) error {
	h.postCommitCalls = append(h.postCommitCalls, hookCall{
		GrantID: grant.Id,
		Type:    grant.Type,
		Granter: grant.Granter,
		Amount:  amount,
	})
	if h.failPostCommitOnAmount != nil && amount.Equal(h.failPostCommitOnAmount) {
		return errors.New("post-commit failure by " + h.name)
	}
	return nil
}

// calls returns the PreCheck call list — used by tests that don't
// care about the PreCheck/PostCommit split (asserting "the hook was
// invoked at all"). Kept as a one-liner for grep-ability.
func (h *recordingHook) calls() []hookCall { return h.preCheckCalls }

func TestClaimHook_RecurringPull_HookInvokedThenSend(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	hook := &recordingHook{name: "rec_pull"}
	f.keeper.SetClaimHooks(hook)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)
	period := int64(86_400)

	id := createRecurringPullGrant(t, f, ms, granter, grantee, amount, period,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	var bankCalls int
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
		bankCalls++
		return nil
	}

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))
	_, err := ms.ClaimRecurringPull(futureCtx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: id,
	})
	require.NoError(t, err)

	// Hook saw PreCheck AND PostCommit, each once, with the right
	// grant + amount.
	require.Len(t, hook.preCheckCalls, 1)
	require.Equal(t, id, hook.preCheckCalls[0].GrantID)
	require.Equal(t, types.GrantType_GRANT_TYPE_RECURRING_PULL, hook.preCheckCalls[0].Type)
	require.Equal(t, granter, hook.preCheckCalls[0].Granter)
	require.True(t, hook.preCheckCalls[0].Amount.Equal(sdk.NewCoins(amount)))
	require.Len(t, hook.postCommitCalls, 1)
	require.Equal(t, id, hook.postCommitCalls[0].GrantID)

	// Bank send happened after PreCheck returned ok.
	require.Equal(t, 1, bankCalls)
}

func TestClaimHook_RecurringPull_VetoBlocksBankSend(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)
	period := int64(86_400)

	id := createRecurringPullGrant(t, f, ms, granter, grantee, amount, period,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	// Register hook AFTER create so create itself isn't blocked.
	hook := &recordingHook{name: "term_expired", vetoOnAmount: sdk.NewCoins(amount)}
	f.keeper.SetClaimHooks(hook)

	var bankCalls int
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
		bankCalls++
		return nil
	}

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))
	_, err := ms.ClaimRecurringPull(futureCtx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: id,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "vetoed by term_expired")

	// Bank send did NOT happen.
	require.Equal(t, 0, bankCalls)
	// PreCheck ran; PostCommit did NOT (veto short-circuits before the
	// bank send).
	require.Len(t, hook.preCheckCalls, 1)
	require.Empty(t, hook.postCommitCalls)
	// Grant state untouched (still ACTIVE, claims_made = 0).
	g, err := f.keeper.Grants.Get(futureCtx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)
	require.Equal(t, uint64(0), g.GetRecurringPull().ClaimsMade)
}

func TestClaimHook_PullAllowance_HookInvoked(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	hook := &recordingHook{name: "allowance"}
	f.keeper.SetClaimHooks(hook)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 5_000_000),
		3_600,
		nil,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	pullAmt := sdk.NewInt64Coin("uspark", 1_500_000)
	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    pullAmt,
	})
	require.NoError(t, err)

	// PreCheck + PostCommit both fired on the SpendingAllowance pull.
	require.Len(t, hook.preCheckCalls, 1)
	require.Equal(t, id, hook.preCheckCalls[0].GrantID)
	require.Equal(t, types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE, hook.preCheckCalls[0].Type)
	require.True(t, hook.preCheckCalls[0].Amount.Equal(sdk.NewCoins(pullAmt)))
	require.Len(t, hook.postCommitCalls, 1)
}

func TestClaimHook_PullAllowance_VetoLeavesGrantUntouched(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 5_000_000),
		3_600,
		nil,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	pullAmt := sdk.NewInt64Coin("uspark", 1_500_000)
	hook := &recordingHook{name: "rate_limit", vetoOnAmount: sdk.NewCoins(pullAmt)}
	f.keeper.SetClaimHooks(hook)

	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    pullAmt,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "vetoed by rate_limit")

	// PreCheck ran and vetoed; PostCommit did NOT run.
	require.Len(t, hook.preCheckCalls, 1)
	require.Empty(t, hook.postCommitCalls)

	g, err := f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	sa := g.GetSpendingAllowance()
	require.NotNil(t, sa)
	// spent_in_current_period untouched on veto.
	require.True(t, sa.SpentInCurrentPeriod.Amount.IsZero())
}

func TestClaimHook_OneshotTransfer_VetoCapturesError(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)

	fireAt := sdkCtx.BlockTime().Add(2 * time.Hour).Unix()
	expiresAt := sdkCtx.BlockTime().Add(24 * time.Hour)
	id, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient, amount, fireAt, expiresAt)

	// Register the veto hook AFTER create so creation isn't affected.
	hook := &recordingHook{name: "council_term_expired", vetoOnAmount: sdk.NewCoins(amount)}
	f.keeper.SetClaimHooks(hook)

	var bankCalls int
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
		bankCalls++
		return nil
	}

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(3*time.Hour))
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(futureCtx)))

	g, err := f.keeper.Grants.Get(futureCtx, id)
	require.NoError(t, err)
	// Oneshot still transitions to FIRED on veto (granter authorized one attempt) but
	// with a captured fire_error and NO bank send.
	require.Equal(t, types.GrantStatus_GRANT_STATUS_FIRED, g.Status)
	require.Contains(t, g.GetScheduledOneshot().FireError, "vetoed by council_term_expired")
	require.Equal(t, 0, bankCalls)
}

func TestClaimHook_OrderingAndShortCircuit(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)
	period := int64(86_400)

	id := createRecurringPullGrant(t, f, ms, granter, grantee, amount, period,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	first := &recordingHook{name: "first"}
	second := &recordingHook{name: "second", vetoOnAmount: sdk.NewCoins(amount)}
	third := &recordingHook{name: "third"}
	f.keeper.SetClaimHooks(first, second, third)

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))
	_, err := ms.ClaimRecurringPull(futureCtx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: id,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "vetoed by second")

	// first ran (1 PreCheck call), second ran and vetoed (1 PreCheck
	// call), third NEVER ran. No PostCommit fired on any of the three.
	require.Len(t, first.preCheckCalls, 1)
	require.Len(t, second.preCheckCalls, 1)
	require.Empty(t, third.preCheckCalls)
	require.Empty(t, first.postCommitCalls)
	require.Empty(t, second.postCommitCalls)
	require.Empty(t, third.postCommitCalls)
}

func TestClaimHook_NoOpWhenNoHooks(t *testing.T) {
	// Sanity: existing tests already cover this implicitly, but call
	// out the no-op path so it's easy to read.
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)
	period := int64(86_400)

	id := createRecurringPullGrant(t, f, ms, granter, grantee, amount, period,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))
	_, err := ms.ClaimRecurringPull(futureCtx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: id,
	})
	require.NoError(t, err)
}

// TestClaimHook_PostCommitFailure_HaltsTx exercises the
// PostCommit-halts-tx contract (plan §3.2 / §5.2): a non-nil error
// returned from PostCommit must bubble out as a handler error so the
// SDK's tx machinery rolls back the bank send and all state writes.
//
// This is exercised at the keeper level only — the unit-test
// invocation doesn't go through baseapp's cache-context, so the
// "bank send rolled back" assertion is necessarily approximate:
// we instead assert (a) the handler returns the PostCommit error,
// and (b) PostCommit fired AFTER PreCheck+bank, i.e. the ordering
// is correct. The end-to-end rollback property follows from the SDK
// contract that any non-nil error from a Msg handler discards the
// cache context.
func TestClaimHook_PostCommitFailure_HaltsTx(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)
	period := int64(86_400)

	id := createRecurringPullGrant(t, f, ms, granter, grantee, amount, period,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	// Hook PreCheck passes; PostCommit fails on the same amount.
	hook := &recordingHook{name: "epoch_budget", failPostCommitOnAmount: sdk.NewCoins(amount)}
	f.keeper.SetClaimHooks(hook)

	var bankCalls int
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
		bankCalls++
		return nil
	}

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))
	_, err := ms.ClaimRecurringPull(futureCtx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: id,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "post-commit failure by epoch_budget")

	// Ordering: PreCheck ran (1), bank send happened (1), PostCommit
	// ran (1) and failed.
	require.Len(t, hook.preCheckCalls, 1)
	require.Equal(t, 1, bankCalls)
	require.Len(t, hook.postCommitCalls, 1)
}

// TestClaimHook_PostCommit_AllowanceFailure_HaltsTx exercises the
// same contract on the SpendingAllowance path.
func TestClaimHook_PostCommit_AllowanceFailure_HaltsTx(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 5_000_000),
		3_600,
		nil,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	pullAmt := sdk.NewInt64Coin("uspark", 1_500_000)
	hook := &recordingHook{name: "rate_limit", failPostCommitOnAmount: sdk.NewCoins(pullAmt)}
	f.keeper.SetClaimHooks(hook)

	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    pullAmt,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "post-commit failure by rate_limit")
	require.Len(t, hook.preCheckCalls, 1)
	require.Len(t, hook.postCommitCalls, 1)
}
