package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
	sessiontypes "sparkdream/x/session/types"
)

// makeGrant returns a synthetic Grant whose Granter is `granter`. The
// hook only branches on Granter (via IsGroupPolicyAddress), so the
// other fields don't need realistic content — the tests directly
// invoke PreCheck / PostCommit, bypassing the session msg server.
func makeGrant(granter, grantee string) sessiontypes.Grant {
	return sessiontypes.Grant{
		Id:      1,
		Type:    sessiontypes.GrantType_GRANT_TYPE_RECURRING_PULL,
		Granter: granter,
		Grantee: grantee,
		Status:  sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE,
	}
}

// registerCouncil seeds a council policy + its name mapping so
// IsGroupPolicyAddress / GetGroupByPolicy resolve.
func registerCouncil(t *testing.T, k keeper.Keeper, ctx sdk.Context, name string, council sdk.AccAddress, group types.Group) {
	t.Helper()
	require.NoError(t, k.Groups.Set(ctx, name, group))
	require.NoError(t, k.PolicyToName.Set(ctx, council.String(), name))
}

// TestSessionClaimHook_NonCouncilGranter_NoOp confirms PreCheck and
// PostCommit are no-ops when the granter is NOT a registered group
// policy. User-to-user RecurringPull grants must pass through
// unaffected.
func TestSessionClaimHook_NonCouncilGranter_NoOp(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	hook := keeper.NewSessionClaimHook(k)
	user := sdk.AccAddress([]byte("regular_user________")).String()
	recipient := sdk.AccAddress([]byte("recipient___________")).String()

	grant := makeGrant(user, recipient)
	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000_000)))

	// Neither PreCheck nor PostCommit should error when the granter
	// isn't a council policy — they short-circuit immediately.
	require.NoError(t, hook.PreCheck(ctx, grant, amt))
	require.NoError(t, hook.PostCommit(ctx, grant, amt))
}

// TestSessionClaimHook_PreCheck_ActivationGate vetoes claims against a
// council still in its pre-launch phase.
func TestSessionClaimHook_PreCheck_ActivationGate(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("hook_shell_council__"))
	limit := math.NewInt(1_000_000)
	now := ctx.BlockTime().Unix()
	registerCouncil(t, k, ctx, "HookShell", council, types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		ActivationTime:        now + 3600, // not yet active
		CurrentTermExpiration: now + 24*3600,
	})

	hook := keeper.NewSessionClaimHook(k)
	grant := makeGrant(council.String(), "recipient")
	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100)))

	err := hook.PreCheck(ctx, grant, amt)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrGroupNotActive)
}

// TestSessionClaimHook_PreCheck_TermExpired vetoes claims against a
// council whose term has elapsed.
func TestSessionClaimHook_PreCheck_TermExpired(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("hook_zombie_council_"))
	limit := math.NewInt(1_000_000)
	now := ctx.BlockTime().Unix()
	registerCouncil(t, k, ctx, "HookZombie", council, types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		CurrentTermExpiration: now - 3600, // term already ended
	})

	hook := keeper.NewSessionClaimHook(k)
	grant := makeGrant(council.String(), "recipient")
	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100)))

	err := hook.PreCheck(ctx, grant, amt)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrGroupExpired)
}

// TestSessionClaimHook_PreCheck_RateLimitSingleTx vetoes a single
// disbursement that exceeds the per-epoch cap.
func TestSessionClaimHook_PreCheck_RateLimitSingleTx(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("hook_rl_council_____"))
	limit := math.NewInt(1000)
	now := ctx.BlockTime().Unix()
	registerCouncil(t, k, ctx, "HookRL", council, types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		CurrentTermExpiration: now + 24*3600,
	})

	hook := keeper.NewSessionClaimHook(k)
	grant := makeGrant(council.String(), "recipient")
	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(2000))) // > 1000 cap

	err := hook.PreCheck(ctx, grant, amt)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)
}

// TestSessionClaimHook_PreCheck_RateLimitCumulative vetoes when the
// cumulative debit (PostCommit'd from a prior claim) plus the new
// request would breach the cap.
func TestSessionClaimHook_PreCheck_RateLimitCumulative(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("hook_rl_cum_council_"))
	limit := math.NewInt(1000)
	now := ctx.BlockTime().Unix()
	registerCouncil(t, k, ctx, "HookRLCum", council, types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		CurrentTermExpiration: now + 24*3600,
	})

	hook := keeper.NewSessionClaimHook(k)
	grant := makeGrant(council.String(), "recipient")
	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(700)))

	// First claim: 700/1000 — passes.
	require.NoError(t, hook.PreCheck(ctx, grant, amt))
	require.NoError(t, hook.PostCommit(ctx, grant, amt))

	// Second claim: would be 1400/1000 — vetoed.
	err := hook.PreCheck(ctx, grant, amt)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)
}

// TestSessionClaimHook_PostCommit_DebitsEpochBucket confirms that
// PostCommit writes to EpochSpending so subsequent PreCheck calls in
// the same epoch see the cumulative total.
func TestSessionClaimHook_PostCommit_DebitsEpochBucket(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("hook_debit_council__"))
	limit := math.NewInt(10_000)
	now := ctx.BlockTime().Unix()
	registerCouncil(t, k, ctx, "HookDebit", council, types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		CurrentTermExpiration: now + 24*3600,
	})

	hook := keeper.NewSessionClaimHook(k)
	grant := makeGrant(council.String(), "recipient")
	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(2_500)))

	// PreCheck only: bucket stays at 0.
	require.NoError(t, hook.PreCheck(ctx, grant, amt))
	// Without PostCommit, a fresh PreCheck for the same amount still
	// passes — the previous "check" was a peek, not a debit.
	require.NoError(t, hook.PreCheck(ctx, grant, amt))

	// Now actually commit. After this, 2500 should be debited.
	require.NoError(t, hook.PostCommit(ctx, grant, amt))

	// Next PreCheck must account for the 2500 already debited.
	// 2500 + 2500 = 5000, still under the 10000 cap → passes.
	require.NoError(t, hook.PreCheck(ctx, grant, amt))
	// 2500 + 8000 > 10000 — vetoed.
	require.ErrorIs(t,
		hook.PreCheck(ctx, grant, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(8_000)))),
		types.ErrRateLimitExceeded)
}

// TestSessionClaimHook_NoLimitCouncil_PassesAndNoOpCommit confirms a
// council with no MaxSpendPerEpoch behaves correctly: PreCheck passes
// any amount, PostCommit is a no-op.
func TestSessionClaimHook_NoLimitCouncil_PassesAndNoOpCommit(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("hook_nolimit_council"))
	now := ctx.BlockTime().Unix()
	registerCouncil(t, k, ctx, "HookNoLimit", council, types.Group{
		PolicyAddress:         council.String(),
		CurrentTermExpiration: now + 24*3600,
		// MaxSpendPerEpoch left nil → no cap
	})

	hook := keeper.NewSessionClaimHook(k)
	grant := makeGrant(council.String(), "recipient")
	huge := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(999_999_999_999)))

	require.NoError(t, hook.PreCheck(ctx, grant, huge))
	require.NoError(t, hook.PostCommit(ctx, grant, huge))
}

// TestSessionClaimHook_DoubleDebitRegression is the §3.2 regression
// test: the pause / topup / retry sequence must debit the per-epoch
// budget exactly ONCE per successful disbursement, even though
// PreCheck runs twice (the pause/resume retry).
//
// Setup: a council with a 1000-uspark cap. The claim path runs
// PreCheck → bank send → PostCommit. The first attempt's bank send
// "fails" (simulated underfunded granter); the second attempt's bank
// send succeeds. With the two-method hook (and tx-level rollback in
// production), only the second attempt's PostCommit should commit
// the debit. The single-method legacy design would have debited in
// PreCheck on the first attempt and again on the second, blowing
// the cap.
//
// Note: unit tests don't go through baseapp's cache-context, so this
// test directly simulates the orchestration: PreCheck → (skip
// PostCommit on bank failure) → PreCheck → PostCommit.
func TestSessionClaimHook_DoubleDebitRegression(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("hook_dd_council_____"))
	limit := math.NewInt(1000)
	now := ctx.BlockTime().Unix()
	registerCouncil(t, k, ctx, "HookDD", council, types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		CurrentTermExpiration: now + 24*3600,
	})

	hook := keeper.NewSessionClaimHook(k)
	grant := makeGrant(council.String(), "recipient")
	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(600)))

	// Attempt 1: PreCheck OK (cap is 1000, request is 600). Bank send
	// fails (we simulate by NOT calling PostCommit).
	require.NoError(t, hook.PreCheck(ctx, grant, amt))
	// Bank send "fails" — PostCommit is skipped. The legacy
	// single-method hook would have already debited 600 here.

	// Attempt 2: granter has topped up; PreCheck must still pass
	// (no spurious debit from the failed attempt 1). Then PostCommit
	// runs.
	require.NoError(t, hook.PreCheck(ctx, grant, amt))
	require.NoError(t, hook.PostCommit(ctx, grant, amt))

	// At this point cumulative debit should be 600 (one successful
	// disbursement), not 1200 (a double-debit).
	// We probe this by trying to PreCheck another 500-uspark spend:
	// 600 + 500 = 1100 > 1000 → veto.
	err := hook.PreCheck(ctx, grant, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(500))))
	require.ErrorIs(t, err, types.ErrRateLimitExceeded,
		"if double-debit had occurred, this would not vet (cumulative would be 1200, request 500 = 1700, still over cap; but the off-by-attempt-1 600 would be missing)")

	// Conversely, a 300-uspark spend should still fit
	// (600 already debited + 300 = 900 ≤ 1000).
	require.NoError(t,
		hook.PreCheck(ctx, grant, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(300)))),
		"single-debit invariant: 600 + 300 ≤ 1000")
}

// TestSessionClaimHook_EpochRollover confirms the debit bucket is
// scoped to the UTC day; the hook follows the same epoch math as
// CheckSpendPreconditions.
func TestSessionClaimHook_EpochRollover(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("hook_rollover_______"))
	limit := math.NewInt(1000)
	now := ctx.BlockTime().Unix()
	registerCouncil(t, k, ctx, "HookRollover", council, types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		CurrentTermExpiration: now + 7*24*3600,
	})

	hook := keeper.NewSessionClaimHook(k)
	grant := makeGrant(council.String(), "recipient")

	full := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000)))
	// Day 1: spend the entire budget.
	require.NoError(t, hook.PreCheck(ctx, grant, full))
	require.NoError(t, hook.PostCommit(ctx, grant, full))
	// Another spend same epoch — vetoed.
	require.ErrorIs(t,
		hook.PreCheck(ctx, grant, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1)))),
		types.ErrRateLimitExceeded)

	// Day 2: roll the block time forward 24h.
	day2 := ctx.WithBlockTime(ctx.BlockTime().Add(25 * time.Hour))
	require.NoError(t, hook.PreCheck(day2, grant, full),
		"fresh epoch should reset the per-epoch cumulative bucket")
}
