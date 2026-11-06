package keeper_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	blogtypes "sparkdream/x/blog/types"
	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// eventRouter dispatches every msg to a handler that emits one event, as a
// real msg server would through its own EventManager.
type eventRouter struct{}

func (eventRouter) Handler(sdk.Msg) baseAppMsgServiceHandler { return eventHandler }
func (eventRouter) HandlerByTypeURL(string) baseAppMsgServiceHandler {
	return eventHandler
}

func eventHandler(_ sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
	return &sdk.Result{Events: []abci.Event{{Type: "inner_msg_ran"}}}, nil
}

// policyCommons models x/commons: alice is an Operations Committee member,
// policy is the committee's policy address.
type policyCommons struct{ member, policy string }

func (c policyCommons) IsCouncilAuthorized(_ context.Context, addr, _, _ string) bool {
	return addr == c.member || addr == c.policy
}

func (c policyCommons) IsCouncilOrCommitteePolicy(_ context.Context, addr, _, _ string) bool {
	return addr == c.policy
}

func execOneshot(t *testing.T, f *fixture, ms types.MsgServer, granter, grantee string, fireAt int64, expiresAt time.Time) uint64 {
	t.Helper()
	inner, err := codectypes.NewAnyWithValue(&blogtypes.MsgCreatePost{Creator: granter})
	require.NoError(t, err)
	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter: granter, Grantee: grantee, ExpiresAt: expiresAt,
		Payload: &types.MsgCreateGrant_ScheduledOneshot{ScheduledOneshot: &types.ScheduledOneshotPayload{
			FireAt: fireAt,
			Action: &types.ScheduledOneshotPayload_Exec{Exec: &types.OneshotExec{Msg: inner, GasLimit: 100_000}},
		}},
	})
	require.NoError(t, err)
	return resp.GrantId
}

// SES-02: the fire pass walks the GrantsByFireTime index, not the grants
// store. An ACTIVE oneshot is indexed at creation and dropped once fired.
func TestSecurity_FirePassUsesIndex(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	fireAt := now.Add(2 * time.Hour).Unix()
	id, _ := createTransferOneshot(t, f, ms, granter, grantee, testAddr("recipient", f.addressCodec),
		sdk.NewInt64Coin("uspark", 1_000_000), fireAt, now.Add(24*time.Hour))

	has, err := f.keeper.GrantsByFireTime.Has(f.ctx, collections.Join(fireAt, id))
	require.NoError(t, err)
	require.True(t, has, "active oneshot is on the fire-time index")

	// Off the index, the grant is not fired: nothing scans the store.
	require.NoError(t, f.keeper.GrantsByFireTime.Remove(f.ctx, collections.Join(fireAt, id)))
	later := withBlockTime(f.ctx, now.Add(3*time.Hour))
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(later)))
	g, err := f.keeper.Grants.Get(later, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)

	// Back on the index it fires, and the entry is consumed.
	require.NoError(t, f.keeper.GrantsByFireTime.Set(f.ctx, collections.Join(fireAt, id)))
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(later)))
	g, err = f.keeper.Grants.Get(later, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_FIRED, g.Status)
	has, err = f.keeper.GrantsByFireTime.Has(f.ctx, collections.Join(fireAt, id))
	require.NoError(t, err)
	require.False(t, has)
}

// SES-04 + SES-06: a oneshot whose msg type was withdrawn from the allowlist
// after scheduling does not fire and its deposit is refunded; an allowed one
// fires and its inner events reach the block.
func TestSecurity_OneshotRechecksAllowlistAtFire(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetRouter(eventRouter{})
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	withdrawnID := execOneshot(t, f, ms, granter, grantee, now.Add(2*time.Hour).Unix(), now.Add(24*time.Hour))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	var narrowed []string
	for _, u := range params.AllowedMsgTypes {
		if u != "/sparkdream.blog.v1.MsgCreatePost" {
			narrowed = append(narrowed, u)
		}
	}
	params.AllowedMsgTypes = narrowed
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	var refunds, feeCollector int
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(context.Context, string, sdk.AccAddress, sdk.Coins) error { refunds++; return nil }
	f.bankKeeper.SendCoinsFromModuleToModuleFn = func(context.Context, string, string, sdk.Coins) error { feeCollector++; return nil }

	later := sdk.UnwrapSDKContext(withBlockTime(f.ctx, now.Add(3*time.Hour)))
	require.NoError(t, f.keeper.EndBlocker(later))
	g, err := f.keeper.Grants.Get(later, withdrawnID)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_FIRED, g.Status)
	require.Contains(t, g.GetScheduledOneshot().FireError, "no longer in params.allowed_msg_types")
	require.Equal(t, 1, refunds, "deposit refunded")
	require.Zero(t, feeCollector, "deposit not consumed")

	// Restore the type: a new oneshot fires and its inner event is visible.
	params.AllowedMsgTypes = append(params.AllowedMsgTypes, "/sparkdream.blog.v1.MsgCreatePost")
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	okID := execOneshot(t, f, ms, granter, testAddr("grantee2", f.addressCodec), now.Add(4*time.Hour).Unix(), now.Add(24*time.Hour))
	later2 := sdk.UnwrapSDKContext(withBlockTime(f.ctx, now.Add(5*time.Hour))).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.EndBlocker(later2))
	g, err = f.keeper.Grants.Get(later2, okID)
	require.NoError(t, err)
	require.Empty(t, g.GetScheduledOneshot().FireError)
	found := false
	for _, e := range later2.EventManager().Events() {
		if e.Type == "inner_msg_ran" {
			found = true
		}
	}
	require.True(t, found, "inner msg events are re-emitted")
}

// SES-03: one grantee can be named by at most MaxGrantsPerGrantee grants.
func TestSecurity_GranteeCap(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	victim := testAddr("victim", f.addressCodec)
	// Each granter is allowed by the victim: the cap bounds consented
	// grants; unconsented ones never reach the class (SES-102).
	signIn := func(i int) error {
		granter := testAddr("sybil"+string(rune('a'+i%26))+string(rune('a'+i/26)), f.addressCodec)
		allowGranter(t, f, victim, granter)
		_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
			Granter: granter, Grantee: victim,
			ExpiresAt: now.Add(time.Hour),
			Payload:   &types.MsgCreateGrant_SignIn{SignIn: &types.SignInPayload{Service: "login.example.org"}},
		})
		return err
	}
	for i := 0; i < keeper.MaxGrantsPerGrantee; i++ {
		require.NoError(t, signIn(i), "grant %d", i)
	}
	require.ErrorIs(t, signIn(keeper.MaxGrantsPerGrantee), types.ErrTooManyGrantsForGrantee)
}

// SES-08: payouts to blocked module accounts are refused at creation and
// at pull time.
func TestSecurity_BlockedRecipients(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	pool := authtypes.NewModuleAddress("bonded_tokens_pool")
	f.bankKeeper.blocked = map[string]bool{pool.String(): true}

	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter: granter, Grantee: grantee, ExpiresAt: now.Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_ScheduledOneshot{ScheduledOneshot: makeTransferOneshot(pool.String(),
			sdk.NewInt64Coin("uspark", 1_000_000), now.Add(2*time.Hour).Unix())},
	})
	require.ErrorIs(t, err, types.ErrRecipientBlocked)

	id := createAllowance(t, f, ms, granter, grantee, sdk.NewInt64Coin("uspark", 10_000_000), 3_600, nil, now.Add(24*time.Hour))
	_, err = ms.PullAllowance(f.ctx, &types.MsgPullAllowance{Grantee: grantee, GrantId: id, Recipient: pool.String(), Amount: sdk.NewInt64Coin("uspark", 1_000_000)})
	require.ErrorIs(t, err, types.ErrRecipientBlocked)
}

// SES-10: an allowance cannot be pulled at or after expires_at, even before
// the expire pass removes it.
func TestSecurity_PullAllowanceRespectsExpiry(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	expires := now.Add(24 * time.Hour)
	id := createAllowance(t, f, ms, granter, grantee, sdk.NewInt64Coin("uspark", 10_000_000), 3_600, nil, expires)
	_, err := ms.PullAllowance(withBlockTime(f.ctx, expires), &types.MsgPullAllowance{
		Grantee: grantee, GrantId: id, Recipient: testAddr("shop", f.addressCodec), Amount: sdk.NewInt64Coin("uspark", 1_000_000),
	})
	require.ErrorIs(t, err, types.ErrGrantExpired)
}

// SES-11: deposits, per-day spend buckets and bypass creators survive an
// export/import round-trip (export -> init -> export is a fixed point).
func TestSecurity_GenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	oneshotID, dep := createTransferOneshot(t, f, ms, granter, grantee, testAddr("recipient", f.addressCodec),
		sdk.NewInt64Coin("uspark", 1_000_000), now.Add(2*time.Hour).Unix(), now.Add(24*time.Hour))

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)
	rpID, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter: testAddr("council", f.addressCodec), Grantee: grantee, ExpiresAt: now.Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{RecurringPull: &types.RecurringPullPayload{
			AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000), PeriodSeconds: 86_400,
		}},
	})
	require.NoError(t, err)
	require.NoError(t, f.keeper.EpochSpendByGrant.Set(f.ctx, collections.Join(rpID, int64(20_000)), "500"))

	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, gs.Validate())
	require.Equal(t, []types.OneshotDeposit{{GrantId: oneshotID, Amount: dep}}, gs.OneshotGasDeposits)
	require.Len(t, gs.EpochSpends, 1)
	require.Equal(t, []types.GrantCreator{{GrantId: rpID, ModuleAddress: caller}}, gs.GrantCreators)

	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *gs))
	gs2, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, gs.OneshotGasDeposits, gs2.OneshotGasDeposits)
	require.Equal(t, gs.EpochSpends, gs2.EpochSpends)
	require.Equal(t, gs.GrantCreators, gs2.GrantCreators)
	has, err := f2.keeper.GrantsByFireTime.Has(f2.ctx, collections.Join(now.Add(2*time.Hour).Unix(), oneshotID))
	require.NoError(t, err)
	require.True(t, has, "fire-time index rebuilt at import")

	// A deposit for a grant that is not a oneshot is refused.
	bad := *gs
	bad.OneshotGasDeposits = []types.OneshotDeposit{{GrantId: rpID, Amount: dep}}
	require.Error(t, bad.Validate())
}

// SES-09: the bypass decline and claim paths act only on grants the calling
// module created (or, for a claim, grants payable to the caller itself).
func TestSecurity_BypassBoundToCreator(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	userID := createRecurringPullGrant(t, f, ms, granter, grantee, sdk.NewInt64Coin("uspark", 1_000_000), 86_400, now.Add(30*24*time.Hour))

	_, err := f.keeper.DeclineGrantInternal(f.ctx, caller, userID, grantee)
	require.ErrorIs(t, err, types.ErrModuleNotAuthorized)
	_, err = f.keeper.ClaimRecurringPullForGrantee(withBlockTime(f.ctx, now.Add(25*time.Hour)), caller, userID, grantee)
	require.ErrorIs(t, err, types.ErrModuleNotAuthorized)

	modID, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter: testAddr("council", f.addressCodec), Grantee: grantee, ExpiresAt: now.Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{RecurringPull: &types.RecurringPullPayload{
			AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000), PeriodSeconds: 86_400,
		}},
	})
	require.NoError(t, err)
	_, err = f.keeper.DeclineGrantInternal(f.ctx, caller, modID, grantee)
	require.NoError(t, err)
}

// SES-09: creation and revocation through the bypass are bound to the
// granters the caller controls (its registered GranterOwner); revocation
// also covers grants the caller created.
func TestSecurity_BypassBoundToControlledGranters(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)
	council := testAddr("council", f.addressCodec)
	f.keeper.SetGranterOwner(caller, onlyGranter(council))
	user, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	pull := &types.MsgCreateGrant_RecurringPull{RecurringPull: &types.RecurringPullPayload{
		AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000), PeriodSeconds: 86_400,
	}}

	// Creating for a granter the caller does not control is refused.
	_, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter: user, Grantee: grantee, ExpiresAt: now.Add(72 * time.Hour), Payload: pull,
	})
	require.ErrorIs(t, err, types.ErrModuleNotAuthorized)

	// A user's own grant cannot be revoked by the module.
	userID := createRecurringPullGrant(t, f, ms, user, grantee, sdk.NewInt64Coin("uspark", 1_000_000), 86_400, now.Add(30*24*time.Hour))
	_, err = f.keeper.RevokeGrantInternal(f.ctx, caller, userID)
	require.ErrorIs(t, err, types.ErrModuleNotAuthorized)

	// A grant the controlled granter made itself (not through the bypass)
	// can be revoked, as can one the caller created.
	councilID := createRecurringPullGrant(t, f, ms, council, grantee, sdk.NewInt64Coin("uspark", 1_000_000), 86_400, now.Add(30*24*time.Hour))
	_, err = f.keeper.RevokeGrantInternal(f.ctx, caller, councilID)
	require.NoError(t, err)
	modID, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter: council, Grantee: grantee, ExpiresAt: now.Add(72 * time.Hour), Payload: pull,
	})
	require.NoError(t, err)
	f.keeper.SetGranterOwner(caller, onlyGranter("nobody"))
	_, err = f.keeper.RevokeGrantInternal(f.ctx, caller, modID)
	require.NoError(t, err, "a grant the caller created stays revocable")
}

// SES-12: the event `type` attribute is the lowercase enum suffix and
// expires_at is RFC3339Nano.
func TestSecurity_EventEncoding(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithEventManager(sdk.NewEventManager())
	now := sdkCtx.BlockTime()
	expires := now.Add(time.Hour)
	_, err := ms.CreateGrant(sdkCtx, &types.MsgCreateGrant{
		Granter: testAddr("granter", f.addressCodec), Grantee: testAddr("grantee", f.addressCodec), ExpiresAt: expires,
		Payload: &types.MsgCreateGrant_SignIn{SignIn: &types.SignInPayload{Service: "login.example.org"}},
	})
	require.NoError(t, err)
	attrs := map[string]string{}
	for _, e := range sdkCtx.EventManager().Events() {
		if e.Type == "grant_created" {
			for _, a := range e.Attributes {
				attrs[a.Key] = a.Value
			}
		}
	}
	require.Equal(t, "sign_in", attrs["type"])
	require.Equal(t, expires.UTC().Format(time.RFC3339Nano), attrs["expires_at"])
	require.Equal(t, "recurring_pull", types.GrantTypeEventValue(types.GrantType_GRANT_TYPE_RECURRING_PULL))
}

// SES-13: stale per-day buckets are pruned by range (not a fixed probe
// window) and every bucket dies with its grant.
func TestSecurity_EpochBucketPruning(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	id := createRecurringPullGrant(t, f, ms, granter, grantee, sdk.NewInt64Coin("uspark", 1_000_000), 86_400, now.Add(30*24*time.Hour))
	day := int64(20_000)
	for _, d := range []int64{day - 400, day - 40, day - 8, day - 2, day} {
		require.NoError(t, f.keeper.EpochSpendByGrant.Set(f.ctx, collections.Join(id, d), "1"))
	}
	f.keeper.PruneStaleEpochBuckets(f.ctx, id, day)
	var left []int64
	require.NoError(t, f.keeper.EpochSpendByGrant.Walk(f.ctx, nil, func(k collections.Pair[uint64, int64], _ string) (bool, error) {
		left = append(left, k.K2())
		return false, nil
	}))
	require.Equal(t, []int64{day - 2, day}, left)

	_, err := ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{Granter: granter, GrantId: id})
	require.NoError(t, err)
	n := 0
	require.NoError(t, f.keeper.EpochSpendByGrant.Walk(f.ctx, nil, func(collections.Pair[uint64, int64], string) (bool, error) { n++; return false, nil }))
	require.Zero(t, n, "buckets removed with the grant")
}

// SES-07: the ops path needs the committee policy (not one member), may
// only add allowed denoms, and every value stays under the compiled
// ceilings.
func TestSecurity_OperationalParamsHardening(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	member, policy := testAddr("ops_member", f.addressCodec), testAddr("ops_policy", f.addressCodec)
	f.keeper.SetCommonsKeeper(policyCommons{member: member, policy: policy})

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.AllowedDenoms = []string{"ibc/ABCDEF"}
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	op := params.ExtractOperationalParams()

	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: member, OperationalParams: op})
	require.ErrorIs(t, err, types.ErrInvalidSigner, "a single committee member is refused")
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: policy, OperationalParams: op})
	require.NoError(t, err)

	removed := op
	removed.AllowedDenoms = nil
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: policy, OperationalParams: removed})
	require.ErrorIs(t, err, types.ErrDenomRemovalForbidden)

	huge := op
	huge.MaxEndblockerDispatchesPerPass = types.MaxParamEndblockerDispatchesPerPass + 1
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: policy, OperationalParams: huge})
	require.ErrorContains(t, err, "exceeds ceiling")

	free := op
	free.OneshotGasPrice = "0"
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: policy, OperationalParams: free})
	require.ErrorContains(t, err, "below the floor")

	spend := op
	spend.MaxSpendLimitAmount = types.MaxParamSpendLimitAmount.Add(sdkmath.OneInt())
	_, err = ms.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: policy, OperationalParams: spend})
	require.ErrorContains(t, err, "exceeds ceiling")
}

// SES-01 (msg server side): the whole batch must fit in the remaining exec
// count; a batch that would overshoot max_exec_count is refused.
func TestSecurity_ExecCountCoversBatch(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetRouter(eventRouter{})
	ms := keeper.NewMsgServerImpl(f.keeper)
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	createTestSessionWithExec(t, f, granter, grantee, []string{"/sparkdream.blog.v1.MsgCreatePost"},
		sdk.UnwrapSDKContext(f.ctx).BlockTime().Add(time.Hour), 2)
	post := func() *codectypes.Any {
		a, err := codectypes.NewAnyWithValue(&blogtypes.MsgCreatePost{Creator: grantee})
		require.NoError(t, err)
		return a
	}
	_, err := ms.ExecSession(f.ctx, &types.MsgExecSession{Granter: granter, Grantee: grantee, Msgs: []*codectypes.Any{post(), post(), post()}})
	require.ErrorIs(t, err, types.ErrExecCountExceeded)
	_, err = ms.ExecSession(f.ctx, &types.MsgExecSession{Granter: granter, Grantee: grantee, Msgs: []*codectypes.Any{post(), post()}})
	require.NoError(t, err)
}

// selfRevokeKey creates a session key for grantee with allow_self_revoke and
// a recurring pull of the same granter for it to target.
func selfRevokeKey(t *testing.T, f *fixture, ms types.MsgServer, granter, grantee string, expires time.Time) (uint64, uint64) {
	t.Helper()
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	allowGranter(t, f, grantee, granter)
	key, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: expires,
		Payload: &types.MsgCreateGrant_SessionKey{SessionKey: &types.SessionKeyPayload{
			AllowedMsgTypes: []string{types.DefaultAllowedMsgTypes[0], types.MsgRevokeGrantTypeURL},
			SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
			MaxExecCount:    10,
			AllowSelfRevoke: true,
		}},
	})
	require.NoError(t, err)
	target, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   testAddr("pull_grantee", f.addressCodec),
		ExpiresAt: now.Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{RecurringPull: &types.RecurringPullPayload{
			AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
			PeriodSeconds:   86_400,
		}},
	})
	require.NoError(t, err)
	return key.GrantId, target.GrantId
}

// SES-16: an expired self-revoke key cannot revoke, even before the expiry
// pass has pruned it; the granter still can.
func TestSecurity_ExpiredSelfRevokeKeyCannotRevoke(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, hot := testAddr("granter", f.addressCodec), testAddr("hot_key", f.addressCodec)
	_, targetID := selfRevokeKey(t, f, ms, granter, hot, now.Add(time.Hour))

	// Past the key's expiry, no EndBlocker run: the key is still ACTIVE.
	later := withBlockTime(f.ctx, now.Add(2*time.Hour))
	_, err := ms.RevokeGrant(later, &types.MsgRevokeGrant{Granter: hot, GrantId: targetID})
	require.ErrorIs(t, err, types.ErrSessionExpired)

	_, err = ms.RevokeGrant(later, &types.MsgRevokeGrant{Granter: granter, GrantId: targetID})
	require.NoError(t, err)
}

// SES-18: a self-revoke scoped into a session works through MsgExecSession
// under the default allowlist, which does not list MsgRevokeGrant.
func TestSecurity_WrappedSelfRevokeUsesFlagNotAllowlist(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetRouter(eventRouter{})
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, hot := testAddr("granter", f.addressCodec), testAddr("hot_key", f.addressCodec)
	_, targetID := selfRevokeKey(t, f, ms, granter, hot, now.Add(24*time.Hour))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.NotContains(t, params.AllowedMsgTypes, types.MsgRevokeGrantTypeURL)

	inner, err := codectypes.NewAnyWithValue(&types.MsgRevokeGrant{Granter: granter, GrantId: targetID})
	require.NoError(t, err)
	ctx := sdk.UnwrapSDKContext(f.ctx).WithEventManager(sdk.NewEventManager())
	_, err = ms.ExecSession(ctx, &types.MsgExecSession{Granter: granter, Grantee: hot, Msgs: []*codectypes.Any{inner}})
	require.NoError(t, err)
	ran := false
	for _, e := range ctx.EventManager().Events() {
		ran = ran || e.Type == "inner_msg_ran"
	}
	require.True(t, ran, "the wrapped revoke was dispatched")
}

// SES-19: genesis validation checks active_grant_counts against the grants
// list instead of trusting it.
func TestSecurity_GenesisActiveCountsCrossValidated(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	selfRevokeKey(t, f, ms, testAddr("granter", f.addressCodec), testAddr("hot_key", f.addressCodec), now.Add(24*time.Hour))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.NotEmpty(t, exported.ActiveGrantCounts)

	mutate := func(fn func(gs *types.GenesisState)) *types.GenesisState {
		gs := *exported
		gs.ActiveGrantCounts = append([]types.ActiveGrantCount(nil), exported.ActiveGrantCounts...)
		fn(&gs)
		return &gs
	}
	for name, gs := range map[string]*types.GenesisState{
		"inflated":    mutate(func(gs *types.GenesisState) { gs.ActiveGrantCounts[0].Count += 5 }),
		"understated": mutate(func(gs *types.GenesisState) { gs.ActiveGrantCounts[0].Count = 0 }),
		"missing":     mutate(func(gs *types.GenesisState) { gs.ActiveGrantCounts = gs.ActiveGrantCounts[1:] }),
		"duplicate": mutate(func(gs *types.GenesisState) {
			gs.ActiveGrantCounts = append(gs.ActiveGrantCounts, gs.ActiveGrantCounts[0])
		}),
	} {
		if name == "missing" && len(exported.ActiveGrantCounts) == 1 {
			// Dropping the only entry leaves an empty list, which is
			// accepted (InitGenesis recomputes).
			require.NoError(t, gs.Validate(), name)
			continue
		}
		require.Error(t, gs.Validate(), name)
	}
}

// SES-17: grant list queries page with next_key. More than one page of
// SignIn grants naming one grantee cannot hide the newest one from a client
// that follows the pages.
func TestSecurity_GrantsByGranteePaginates(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	victim := testAddr("victim_key", f.addressCodec)

	// Six allowed granters at the per-granter SignIn cap: 120 grants.
	for g := 0; g < 6; g++ {
		granter := testAddr(fmt.Sprintf("spam_%d", g), f.addressCodec)
		allowGranter(t, f, victim, granter)
		for s := 0; s < types.MaxSignInGrantsPerGranter; s++ {
			_, err := ms.CreateGrant(f.ctx, signInGrant(granter, victim, fmt.Sprintf("s%d.example", s), now.Add(24*time.Hour)))
			require.NoError(t, err)
		}
	}
	allowGranter(t, f, victim, testAddr("real_account", f.addressCodec))
	real, err := ms.CreateGrant(f.ctx, signInGrant(testAddr("real_account", f.addressCodec), victim, "login.example", now.Add(24*time.Hour)))
	require.NoError(t, err)

	var all []types.Grant
	var key []byte
	pages := 0
	for {
		resp, err := qs.GrantsByGrantee(f.ctx, &types.QueryGrantsByGranteeRequest{
			Grantee: victim, Type: types.GrantType_GRANT_TYPE_SIGN_IN,
			Pagination: &query.PageRequest{Key: key},
		})
		require.NoError(t, err)
		require.LessOrEqual(t, len(resp.Grants), 100)
		all = append(all, resp.Grants...)
		pages++
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		key = resp.Pagination.NextKey
	}
	require.Equal(t, 2, pages)
	require.Len(t, all, 6*types.MaxSignInGrantsPerGranter+1)
	require.Equal(t, real.GrantId, all[len(all)-1].Id, "the real grant is on the last page")
}

// revokeRouter dispatches MsgRevokeGrant to the session msg server, so a
// wrapped revoke really runs.
type revokeRouter struct{ ms types.MsgServer }

func (r revokeRouter) Handler(sdk.Msg) baseAppMsgServiceHandler { return r.handle }
func (r revokeRouter) HandlerByTypeURL(string) baseAppMsgServiceHandler {
	return r.handle
}
func (r revokeRouter) handle(ctx sdk.Context, msg sdk.Msg) (*sdk.Result, error) {
	if _, err := r.ms.RevokeGrant(ctx, msg.(*types.MsgRevokeGrant)); err != nil {
		return nil, err
	}
	return &sdk.Result{}, nil
}

// A self-revoking key that revokes itself through MsgExecSession stays
// revoked: ExecSession used to write back the grant it loaded before the
// inner revoke ran, resurrecting it as an unindexed ACTIVE orphan.
func TestSecurity_ExecSessionSelfRevokeStaysRevoked(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	f.keeper.SetRouter(revokeRouter{ms: ms})
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, hot := testAddr("granter", f.addressCodec), testAddr("hot_key", f.addressCodec)
	keyID, _ := selfRevokeKey(t, f, ms, granter, hot, now.Add(24*time.Hour))

	inner, err := codectypes.NewAnyWithValue(&types.MsgRevokeGrant{Granter: granter, GrantId: keyID})
	require.NoError(t, err)
	_, err = ms.ExecSession(f.ctx, &types.MsgExecSession{Granter: granter, Grantee: hot, Msgs: []*codectypes.Any{inner}})
	require.NoError(t, err)

	g, err := f.keeper.Grants.Get(f.ctx, keyID)
	if err == nil {
		require.NotEqual(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status, "the revoked key must not come back ACTIVE")
	}
}
