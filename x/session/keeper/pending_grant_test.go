package keeper_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoany "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"

	blogtypes "sparkdream/x/blog/types"
	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// Grantee consent for access-class grants (SES-102).

// depositLedger records the bank moves the consent flow makes: escrow into
// the module, refunds out of it, and forfeits to the community pool / fee
// collector.
type depositLedger struct {
	escrowed  sdk.Coins
	refunded  map[string]sdk.Coins
	toModule  sdk.Coins
	pool      sdk.Coins
	poolError error
}

func (l *depositLedger) FundCommunityPool(_ context.Context, amount sdk.Coins, _ sdk.AccAddress) error {
	if l.poolError != nil {
		return l.poolError
	}
	l.pool = l.pool.Add(amount...)
	return nil
}

func trackDeposits(f *fixture) *depositLedger {
	l := &depositLedger{refunded: map[string]sdk.Coins{}}
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(_ context.Context, _ sdk.AccAddress, module string, amt sdk.Coins) error {
		if module == types.ModuleName {
			l.escrowed = l.escrowed.Add(amt...)
		}
		return nil
	}
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, to sdk.AccAddress, amt sdk.Coins) error {
		l.refunded[to.String()] = l.refunded[to.String()].Add(amt...)
		return nil
	}
	f.bankKeeper.SendCoinsFromModuleToModuleFn = func(_ context.Context, _, _ string, amt sdk.Coins) error {
		l.toModule = l.toModule.Add(amt...)
		return nil
	}
	f.keeper.SetDistrKeeper(l)
	return l
}

func depositCoins() sdk.Coins {
	return sdk.NewCoins(sdk.NewInt64Coin("uspark", int64(types.DefaultPendingGrantDeposit)))
}

func sessionKeyGrantMsg(granter, grantee string, exp time.Time) *types.MsgCreateGrant {
	return &types.MsgCreateGrant{
		Granter: granter, Grantee: grantee, ExpiresAt: exp,
		Payload: &types.MsgCreateGrant_SessionKey{SessionKey: &types.SessionKeyPayload{
			AllowedMsgTypes: []string{"/sparkdream.blog.v1.MsgCreatePost"},
			SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
			MaxExecCount:    10,
		}},
	}
}

func execMsg(t *testing.T, granter, grantee string) *types.MsgExecSession {
	t.Helper()
	inner, err := codectypes.NewAnyWithValue(&blogtypes.MsgCreatePost{Creator: granter})
	require.NoError(t, err)
	return &types.MsgExecSession{
		Granter: granter, Grantee: grantee,
		Msgs: []*gogoany.Any{{TypeUrl: inner.TypeUrl, Value: inner.Value}},
	}
}

func granteeIndexCount(t *testing.T, f *fixture, grantee string) int {
	t.Helper()
	all, err := f.keeper.ListGrantsByGrantee(f.ctx, grantee, types.GrantType_GRANT_TYPE_UNSPECIFIED)
	require.NoError(t, err)
	return len(all)
}

func requireInvariants(t *testing.T, f *fixture) {
	t.Helper()
	ctx := sdk.UnwrapSDKContext(f.ctx)
	for name, inv := range map[string]sdk.Invariant{
		"grant-indexes":       keeper.GrantIndexesInvariant(f.keeper),
		"active-grant-counts": keeper.ActiveGrantCountsInvariant(f.keeper),
		"pending-grants":      keeper.PendingGrantsInvariant(f.keeper),
	} {
		msg, broken := inv(ctx)
		require.False(t, broken, "%s: %s", name, msg)
	}
}

// An access-class grant from an unknown granter is PENDING: escrowed, out
// of every grantee index, not usable through the legacy session lookup's
// consumers, and the granter is not allowed yet.
func TestPendingGrant_UnknownGranterIsPending(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("stranger", f.addressCodec), testAddr("hot_key", f.addressCodec)

	resp, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, grantee, now.Add(24*time.Hour)))
	require.NoError(t, err)
	g, err := f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PENDING, g.Status)
	require.Equal(t, depositCoins(), ledger.escrowed, "deposit escrowed")
	dep, err := f.keeper.PendingGrantDeposit.Get(f.ctx, g.Id)
	require.NoError(t, err)
	require.Equal(t, depositCoins()[0], dep)

	// Out of the grantee indexes and the class cap; under the granter.
	require.Zero(t, granteeIndexCount(t, f, grantee))
	byGrantee, err := qs.GrantsByGrantee(f.ctx, &types.QueryGrantsByGranteeRequest{Grantee: grantee})
	require.NoError(t, err)
	require.Empty(t, byGrantee.Grants)
	byGranter, err := f.keeper.ListGrantsByGranter(f.ctx, granter, types.GrantType_GRANT_TYPE_UNSPECIFIED)
	require.NoError(t, err)
	require.Len(t, byGranter, 1)
	has, err := f.keeper.GrantsByExpiration.Has(f.ctx, collections.Join(g.ExpiresAt.Unix(), g.Id))
	require.NoError(t, err)
	require.False(t, has, "a pending grant is not on the expiration index")
	n, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
	require.NoError(t, err)
	require.Zero(t, n, "pending grants are not active")
	pendingN, err := f.keeper.CountPendingGrants(f.ctx, granter)
	require.NoError(t, err)
	require.Equal(t, uint32(1), pendingN)
	allowed, err := f.keeper.IsGranterAllowed(f.ctx, grantee, granter)
	require.NoError(t, err)
	require.False(t, allowed)
	// One key per pair, pending or not.
	_, err = ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, grantee, now.Add(24*time.Hour)))
	require.ErrorIs(t, err, types.ErrSessionExists)
	requireInvariants(t, f)
}

// The grantee's first exec accepts the key and executes in one tx; the
// second exec needs no acceptance; the granter is now allowed.
func TestPendingGrant_FirstExecAccepts(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetRouter(eventRouter{})
	ms := keeper.NewMsgServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("owner", f.addressCodec), testAddr("hot_key", f.addressCodec)

	resp, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, grantee, now.Add(24*time.Hour)))
	require.NoError(t, err)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithEventManager(sdk.NewEventManager())
	_, err = ms.ExecSession(sdkCtx, execMsg(t, granter, grantee))
	require.NoError(t, err)
	var accepted, executed bool
	for _, ev := range sdkCtx.EventManager().Events() {
		accepted = accepted || ev.Type == "grant_accepted"
		executed = executed || ev.Type == "inner_msg_ran"
	}
	require.True(t, accepted, "grant_accepted emitted")
	require.True(t, executed, "inner message ran in the same tx")

	g, err := f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)
	require.Equal(t, uint64(1), g.GetSessionKey().ExecCount)
	require.Equal(t, 1, granteeIndexCount(t, f, grantee))
	require.Equal(t, depositCoins(), ledger.refunded[granter], "deposit refunded to the granter")
	_, err = f.keeper.PendingGrantDeposit.Get(f.ctx, g.Id)
	require.ErrorIs(t, err, collections.ErrNotFound)
	allowed, err := f.keeper.IsGranterAllowed(f.ctx, grantee, granter)
	require.NoError(t, err)
	require.True(t, allowed)
	pendingN, err := f.keeper.CountPendingGrants(f.ctx, granter)
	require.NoError(t, err)
	require.Zero(t, pendingN)
	n, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
	require.NoError(t, err)
	require.Equal(t, uint32(1), n)

	// Second exec: plain execution.
	_, err = ms.ExecSession(f.ctx, execMsg(t, granter, grantee))
	require.NoError(t, err)
	g, err = f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	require.Equal(t, uint64(2), g.GetSessionKey().ExecCount)
	// Explicit accept of an active grant is refused.
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: grantee, GrantId: resp.GrantId})
	require.ErrorIs(t, err, types.ErrGrantNotPending)

	// Once allowed, the granter's next key to this grantee is active at
	// creation (after the first is revoked: one per pair).
	_, err = ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{Granter: granter, GrantId: resp.GrantId})
	require.NoError(t, err)
	resp2, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, grantee, now.Add(24*time.Hour)))
	require.NoError(t, err)
	g2, err := f.keeper.Grants.Get(f.ctx, resp2.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g2.Status)
	require.Equal(t, depositCoins(), ledger.escrowed, "no second deposit")
	requireInvariants(t, f)
}

// A sign-in grant is accepted only explicitly; sdaplogin's by_grantee
// listing sees it after the accept.
func TestPendingGrant_SignInExplicitAccept(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("member", f.addressCodec), testAddr("laptop", f.addressCodec)

	resp, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "mstdn.sparkdream.io", now.Add(24*time.Hour)))
	require.NoError(t, err)
	listed, err := qs.GrantsByGrantee(f.ctx, &types.QueryGrantsByGranteeRequest{Grantee: grantee, Type: types.GrantType_GRANT_TYPE_SIGN_IN})
	require.NoError(t, err)
	require.Empty(t, listed.Grants, "a pending sign-in is invisible to the service")
	// A duplicate (grantee, service) is refused while pending.
	_, err = ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "mstdn.sparkdream.io", now.Add(24*time.Hour)))
	require.ErrorIs(t, err, types.ErrSignInGrantExists)

	// Only the grantee may accept.
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: granter, GrantId: resp.GrantId})
	require.ErrorIs(t, err, types.ErrAcceptUnauthorized)
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: grantee, GrantId: 404})
	require.ErrorIs(t, err, types.ErrGrantNotFound)

	acc, err := ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: grantee, GrantId: resp.GrantId})
	require.NoError(t, err)
	require.Equal(t, depositCoins()[0], acc.RefundAmount)
	require.Equal(t, depositCoins(), ledger.refunded[granter])
	listed, err = qs.GrantsByGrantee(f.ctx, &types.QueryGrantsByGranteeRequest{Grantee: grantee, Type: types.GrantType_GRANT_TYPE_SIGN_IN})
	require.NoError(t, err)
	require.Len(t, listed.Grants, 1)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, listed.Grants[0].Status)
	allowedList, err := qs.AllowedGranters(f.ctx, &types.QueryAllowedGrantersRequest{Grantee: grantee})
	require.NoError(t, err)
	require.Equal(t, []string{granter}, allowedList.Granters)
	requireInvariants(t, f)
}

// A granter holds at most MaxPendingGrantsPerGranter pending grants.
func TestPendingGrant_PerGranterCap(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter := testAddr("spammer", f.addressCodec)
	for i := 0; i < types.MaxPendingGrantsPerGranter; i++ {
		_, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, testAddr(fmt.Sprintf("key%d", i), f.addressCodec), now.Add(time.Hour)))
		require.NoError(t, err, "pending %d", i)
	}
	_, err := ms.CreateGrant(f.ctx, signInGrant(granter, testAddr("key_more", f.addressCodec), "login.example", now.Add(time.Hour)))
	require.ErrorIs(t, err, types.ErrMaxPendingGrantsExceeded)
	_, err = ms.CreateSession(f.ctx, &types.MsgCreateSession{
		Granter: granter, Grantee: testAddr("key_legacy", f.addressCodec), AllowedMsgTypes: []string{"/sparkdream.blog.v1.MsgCreatePost"},
		SpendLimit: sdk.NewInt64Coin("uspark", 1_000_000), Expiration: now.Add(time.Hour), MaxExecCount: 10,
	})
	require.ErrorIs(t, err, types.ErrMaxPendingGrantsExceeded, "legacy path shares the cap")
	// Allowed grantees are unaffected by the cap.
	allowGranter(t, f, testAddr("friend", f.addressCodec), granter)
	_, err = ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, testAddr("friend", f.addressCodec), now.Add(time.Hour)))
	require.NoError(t, err)
	requireInvariants(t, f)
}

// Acceptance re-runs the grantee's class cap: over cap the grant stays
// pending and the deposit stays escrowed.
func TestPendingGrant_AcceptOverGranteeCapStaysPending(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	grantee := testAddr("busy_key", f.addressCodec)
	for i := 0; i < keeper.MaxGrantsPerGrantee; i++ {
		granter := testAddr(fmt.Sprintf("ok%03d", i), f.addressCodec)
		allowGranter(t, f, grantee, granter)
		_, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "login.example", now.Add(time.Hour)))
		require.NoError(t, err, "grant %d", i)
	}
	newcomer := testAddr("newcomer", f.addressCodec)
	resp, err := ms.CreateGrant(f.ctx, signInGrant(newcomer, grantee, "login.example", now.Add(time.Hour)))
	require.NoError(t, err, "creation is not capped: the grant is pending")
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: grantee, GrantId: resp.GrantId})
	require.ErrorIs(t, err, types.ErrTooManyGrantsForGrantee)
	g, err := f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PENDING, g.Status)
	require.Empty(t, ledger.refunded[newcomer])
	allowed, err := f.keeper.IsGranterAllowed(f.ctx, grantee, newcomer)
	require.NoError(t, err)
	require.False(t, allowed)
	requireInvariants(t, f)
}

// Revoke and decline of a pending grant refund the granter.
func TestPendingGrant_RevokeAndDeclineRefund(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter := testAddr("granter", f.addressCodec)
	k1, k2 := testAddr("key1", f.addressCodec), testAddr("key2", f.addressCodec)

	r1, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, k1, now.Add(time.Hour)))
	require.NoError(t, err)
	r2, err := ms.CreateGrant(f.ctx, signInGrant(granter, k2, "login.example", now.Add(time.Hour)))
	require.NoError(t, err)

	rev, err := ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{Granter: granter, GrantId: r1.GrantId})
	require.NoError(t, err)
	require.Equal(t, depositCoins()[0], rev.RefundAmount)
	// A stranger cannot decline; the grantee can, and the granter is refunded.
	_, err = ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{Grantee: k1, GrantId: r2.GrantId})
	require.ErrorIs(t, err, types.ErrDeclineUnauthorized)
	dec, err := ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{Grantee: k2, GrantId: r2.GrantId})
	require.NoError(t, err)
	require.Equal(t, depositCoins()[0], dec.RefundAmount)
	require.Equal(t, depositCoins().Add(depositCoins()...), ledger.refunded[granter])
	for _, id := range []uint64{r1.GrantId, r2.GrantId} {
		_, err = f.keeper.Grants.Get(f.ctx, id)
		require.ErrorIs(t, err, collections.ErrNotFound)
		_, err = f.keeper.PendingGrantDeposit.Get(f.ctx, id)
		require.ErrorIs(t, err, collections.ErrNotFound)
	}
	pendingN, err := f.keeper.CountPendingGrants(f.ctx, granter)
	require.NoError(t, err)
	require.Zero(t, pendingN)
	// Declining does not allow the granter.
	allowed, err := f.keeper.IsGranterAllowed(f.ctx, k2, granter)
	require.NoError(t, err)
	require.False(t, allowed)
	// A pending session key cannot act as a self-revoke key.
	r3, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter: granter, Grantee: k1, ExpiresAt: now.Add(time.Hour),
		Payload: &types.MsgCreateGrant_SessionKey{SessionKey: &types.SessionKeyPayload{
			AllowedMsgTypes: []string{"/sparkdream.blog.v1.MsgCreatePost"}, SpendLimit: sdk.NewInt64Coin("uspark", 1_000_000),
			MaxExecCount: 10, AllowSelfRevoke: true,
		}},
	})
	require.NoError(t, err)
	_, err = ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{Granter: k1, GrantId: r3.GrantId})
	require.ErrorIs(t, err, types.ErrRevokeUnauthorized)
	requireInvariants(t, f)
}

// The EndBlocker drops a pending grant past the TTL (or its own expiry,
// whichever is first) and forfeits the deposit to the community pool; the
// fee collector is the fallback.
func TestPendingGrant_ExpiryForfeits(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter := testAddr("granter", f.addressCodec)
	ttl := time.Duration(types.PendingGrantTTLSeconds) * time.Second

	long, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, testAddr("long", f.addressCodec), now.Add(2*ttl+time.Hour)))
	require.NoError(t, err)
	short, err := ms.CreateGrant(f.ctx, signInGrant(granter, testAddr("short", f.addressCodec), "login.example", now.Add(ttl/2)))
	require.NoError(t, err)

	// Before either deadline nothing happens.
	early := sdk.UnwrapSDKContext(withBlockTime(f.ctx, now.Add(ttl/4)))
	require.NoError(t, f.keeper.EndBlocker(early))
	require.Empty(t, ledger.pool)
	// The grantee cannot accept an expired grant even before the pass
	// prunes it.
	lateCtx := withBlockTime(f.ctx, now.Add(ttl/2+time.Minute))
	_, err = ms.AcceptGrant(lateCtx, &types.MsgAcceptGrant{Grantee: testAddr("short", f.addressCodec), GrantId: short.GrantId})
	require.ErrorIs(t, err, types.ErrGrantExpired)

	// Own expiry first: the short grant goes, the long one stays.
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(lateCtx)))
	_, err = f.keeper.Grants.Get(f.ctx, short.GrantId)
	require.ErrorIs(t, err, collections.ErrNotFound)
	_, err = f.keeper.Grants.Get(f.ctx, long.GrantId)
	require.NoError(t, err)
	require.Equal(t, depositCoins(), ledger.pool, "deposit forfeited to the community pool")
	require.Empty(t, ledger.refunded[granter])

	// TTL: the long grant goes at created_at + TTL, well before its expiry;
	// with the pool refusing, the fee collector takes the deposit.
	ledger.poolError = fmt.Errorf("pool closed")
	after := sdk.UnwrapSDKContext(withBlockTime(f.ctx, now.Add(ttl+time.Second)))
	require.NoError(t, f.keeper.EndBlocker(after))
	_, err = f.keeper.Grants.Get(f.ctx, long.GrantId)
	require.ErrorIs(t, err, collections.ErrNotFound)
	require.Equal(t, depositCoins(), ledger.toModule, "fallback to the fee collector")
	_, err = f.keeper.PendingGrantDeposit.Get(f.ctx, long.GrantId)
	require.ErrorIs(t, err, collections.ErrNotFound)
	pendingN, err := f.keeper.CountPendingGrants(f.ctx, granter)
	require.NoError(t, err)
	require.Zero(t, pendingN)
	requireInvariants(t, f)

	// The pass is capped per block.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxEndblockerDispatchesPerPass = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	for i := 0; i < 3; i++ {
		_, err := ms.CreateGrant(after, sessionKeyGrantMsg(granter, testAddr(fmt.Sprintf("k%d", i), f.addressCodec), after.BlockTime().Add(time.Hour)))
		require.NoError(t, err)
	}
	later := sdk.UnwrapSDKContext(withBlockTime(f.ctx, after.BlockTime().Add(2*time.Hour)))
	require.NoError(t, f.keeper.EndBlocker(later))
	pendingN, err = f.keeper.CountPendingGrants(later, granter)
	require.NoError(t, err)
	require.Equal(t, uint32(1), pendingN, "two of three pruned in one block")
	require.NoError(t, f.keeper.EndBlocker(later))
	pendingN, err = f.keeper.CountPendingGrants(later, granter)
	require.NoError(t, err)
	require.Zero(t, pendingN)
}

// MsgDisallowGranter removes consent; existing grants stay.
func TestPendingGrant_DisallowGranter(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("key", f.addressCodec)

	_, err := ms.DisallowGranter(f.ctx, &types.MsgDisallowGranter{Grantee: grantee, Granter: granter})
	require.ErrorIs(t, err, types.ErrGranterNotAllowed)

	resp, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "login.example", now.Add(time.Hour)))
	require.NoError(t, err)
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: grantee, GrantId: resp.GrantId})
	require.NoError(t, err)
	_, err = ms.DisallowGranter(f.ctx, &types.MsgDisallowGranter{Grantee: grantee, Granter: granter})
	require.NoError(t, err)
	allowed, err := f.keeper.IsGranterAllowed(f.ctx, grantee, granter)
	require.NoError(t, err)
	require.False(t, allowed)
	has, err := f.keeper.AllowedGranteesByGranter.Has(f.ctx, collections.Join(granter, grantee))
	require.NoError(t, err)
	require.False(t, has)
	g, err := f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status, "existing grant untouched")
	// The next grant waits again.
	next, err := ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "other.example", now.Add(time.Hour)))
	require.NoError(t, err)
	g, err = f.keeper.Grants.Get(f.ctx, next.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PENDING, g.Status)
	requireInvariants(t, f)
}

// Module-bypass grants and payout-class grants never wait; a zero deposit
// param keeps the consent step without escrow.
func TestPendingGrant_BypassAndPayoutClassUnchanged(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	commons := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, commons)
	policy, daemon := testAddr("policy", f.addressCodec), testAddr("daemon", f.addressCodec)

	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, commons, sessionKeyGrantMsg(policy, daemon, now.Add(time.Hour)))
	require.NoError(t, err)
	g, err := f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status, "bypass grants are active at creation")
	require.Empty(t, ledger.escrowed)

	stranger := testAddr("stranger", f.addressCodec)
	pull, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter: stranger, Grantee: daemon, ExpiresAt: now.Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{RecurringPull: &types.RecurringPullPayload{
			AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000), PeriodSeconds: 86_400,
		}},
	})
	require.NoError(t, err)
	g, err = f.keeper.Grants.Get(f.ctx, pull.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status, "payout grants are active at creation")
	require.Equal(t, 2, granteeIndexCount(t, f, daemon))
	require.Empty(t, ledger.escrowed)
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: daemon, GrantId: pull.GrantId})
	require.ErrorIs(t, err, types.ErrGrantNotPending)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.PendingGrantDeposit = 0
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	free, err := ms.CreateGrant(f.ctx, signInGrant(stranger, daemon, "login.example", now.Add(time.Hour)))
	require.NoError(t, err)
	g, err = f.keeper.Grants.Get(f.ctx, free.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PENDING, g.Status)
	require.Empty(t, ledger.escrowed)
	_, err = f.keeper.PendingGrantDeposit.Get(f.ctx, free.GrantId)
	require.ErrorIs(t, err, collections.ErrNotFound)
	acc, err := ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: daemon, GrantId: free.GrantId})
	require.NoError(t, err)
	require.True(t, acc.RefundAmount.IsZero())
	requireInvariants(t, f)
}

// The SES-102 scenario: 30 sybil granters push keys at a public hot key;
// the owner's renewal (owner allowed) succeeds and the hot key's grantee
// index holds only the owner's grants.
func TestPendingGrant_SybilsCannotFillHotKey(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetRouter(eventRouter{})
	ms := keeper.NewMsgServerImpl(f.keeper)
	trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	hot := testAddr("daemon_hot_key", f.addressCodec)
	owner := testAddr("operator", f.addressCodec)

	for i := 0; i < 30; i++ {
		sybil := testAddr(fmt.Sprintf("sybil%02d", i), f.addressCodec)
		_, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(sybil, hot, now.Add(24*time.Hour)))
		require.NoError(t, err)
		for j := 0; j < types.MaxPendingGrantsPerGranter-1; j++ {
			_, err := ms.CreateGrant(f.ctx, signInGrant(sybil, hot, fmt.Sprintf("s%d.example", j), now.Add(24*time.Hour)))
			require.NoError(t, err)
		}
		_, err = ms.CreateGrant(f.ctx, signInGrant(sybil, hot, "one-more.example", now.Add(24*time.Hour)))
		require.ErrorIs(t, err, types.ErrMaxPendingGrantsExceeded)
	}
	require.Zero(t, granteeIndexCount(t, f, hot), "nothing reached the hot key's indexes")

	// First-ever key from the owner: accepted by the daemon's first exec.
	first, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(owner, hot, now.Add(24*time.Hour)))
	require.NoError(t, err)
	_, err = ms.ExecSession(f.ctx, execMsg(t, owner, hot))
	require.NoError(t, err)
	// Renewal: revoke the old key, the new one is active at creation.
	_, err = ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{Granter: owner, GrantId: first.GrantId})
	require.NoError(t, err)
	renewed, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(owner, hot, now.Add(24*time.Hour)))
	require.NoError(t, err)
	g, err := f.keeper.Grants.Get(f.ctx, renewed.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)
	all, err := f.keeper.ListGrantsByGrantee(f.ctx, hot, types.GrantType_GRANT_TYPE_UNSPECIFIED)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, owner, all[0].Granter)
	requireInvariants(t, f)
}

// Genesis round trip carries pending grants, their deposits and the
// allowed-granter entries; the invariants hold on import.
func TestPendingGrant_GenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter := testAddr("granter", f.addressCodec)
	k1, k2 := testAddr("key1", f.addressCodec), testAddr("key2", f.addressCodec)

	pendingResp, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, k1, now.Add(time.Hour)))
	require.NoError(t, err)
	acceptedResp, err := ms.CreateGrant(f.ctx, signInGrant(granter, k2, "login.example", now.Add(time.Hour)))
	require.NoError(t, err)
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: k2, GrantId: acceptedResp.GrantId})
	require.NoError(t, err)

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.PendingGrantDeposits, 1)
	require.Equal(t, pendingResp.GrantId, exported.PendingGrantDeposits[0].GrantId)
	require.Equal(t, []types.AllowedGranter{{Grantee: k2, Granter: granter}}, exported.AllowedGranters)

	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))
	requireInvariants(t, f2)
	g, err := f2.keeper.Grants.Get(f2.ctx, pendingResp.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PENDING, g.Status)
	require.Zero(t, granteeIndexCount(t, f2, k1))
	pendingN, err := f2.keeper.CountPendingGrants(f2.ctx, granter)
	require.NoError(t, err)
	require.Equal(t, uint32(1), pendingN, "pending counts recomputed on import")
	allowed, err := f2.keeper.IsGranterAllowed(f2.ctx, k2, granter)
	require.NoError(t, err)
	require.True(t, allowed)
	exported2, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, exported.PendingGrantDeposits, exported2.PendingGrantDeposits)
	require.Equal(t, exported.AllowedGranters, exported2.AllowedGranters)

	// Validate refuses what the keeper never writes.
	bad := *exported
	bad.PendingGrantDeposits = append(bad.PendingGrantDeposits, types.PendingGrantDeposit{GrantId: acceptedResp.GrantId, Amount: depositCoins()[0]})
	require.ErrorContains(t, bad.Validate(), "not a pending grant")
	bad = *exported
	bad.Grants = append([]types.Grant(nil), exported.Grants...)
	for i := range bad.Grants {
		if bad.Grants[i].Id == acceptedResp.GrantId {
			bad.Grants[i].Status = types.GrantStatus_GRANT_STATUS_PENDING
		}
	}
	for i := 0; i < types.MaxPendingGrantsPerGranter; i++ {
		extra := sessionKeyGrant(100+uint64(i), granter, testAddr(fmt.Sprintf("x%d", i), f.addressCodec), []string{"/sparkdream.blog.v1.MsgCreatePost"},
			sdk.NewInt64Coin("uspark", 1_000_000), sdk.NewInt64Coin("uspark", 0), now.Add(time.Hour), now, 0, 10)
		extra.Status = types.GrantStatus_GRANT_STATUS_PENDING
		bad.Grants = append(bad.Grants, extra)
	}
	bad.GrantSeq = 200
	bad.ActiveGrantCounts = nil
	require.ErrorContains(t, bad.Validate(), "pending grants")
	bad = *exported
	bad.AllowedGranters = append(bad.AllowedGranters, types.AllowedGranter{Grantee: k2, Granter: granter})
	require.ErrorContains(t, bad.Validate(), "duplicate")
}

// The member-migration hook re-keys the granter side of consent on the KEY
// path, drops it otherwise, and refunds a pending deposit to the new key.
func TestPendingGrant_MigrationHooks(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	oldAddr, newAddr := testAddr("old_member", f.addressCodec), testAddr("new_member", f.addressCodec)
	k1, k2 := testAddr("key1", f.addressCodec), testAddr("key2", f.addressCodec)
	oldAcc, err := f.addressCodec.StringToBytes(oldAddr)
	require.NoError(t, err)
	newAcc, err := f.addressCodec.StringToBytes(newAddr)
	require.NoError(t, err)

	accepted, err := ms.CreateGrant(f.ctx, signInGrant(oldAddr, k1, "login.example", now.Add(time.Hour)))
	require.NoError(t, err)
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: k1, GrantId: accepted.GrantId})
	require.NoError(t, err)
	_, err = ms.CreateGrant(f.ctx, sessionKeyGrantMsg(oldAddr, k2, now.Add(time.Hour)))
	require.NoError(t, err)

	hooks := keeper.NewMigrationHooks(&f.keeper)
	require.NoError(t, hooks.AfterMemberMigration(f.ctx, oldAcc, newAcc, reptypes.MigrationPath_MIGRATION_PATH_KEY))
	allowed, err := f.keeper.IsGranterAllowed(f.ctx, k1, newAddr)
	require.NoError(t, err)
	require.True(t, allowed, "consent re-keyed to the new address")
	allowed, err = f.keeper.IsGranterAllowed(f.ctx, k1, oldAddr)
	require.NoError(t, err)
	require.False(t, allowed)
	pendingN, err := f.keeper.CountPendingGrants(f.ctx, newAddr)
	require.NoError(t, err)
	require.Equal(t, uint32(1), pendingN, "pending key re-keyed with its granter")
	requireInvariants(t, f)

	// RECOVERY: the consent is dropped and the pending key is revoked with
	// its deposit refunded to the recovered address.
	f = initFixture(t)
	ms = keeper.NewMsgServerImpl(f.keeper)
	ledger := trackDeposits(f)
	accepted, err = ms.CreateGrant(f.ctx, signInGrant(oldAddr, k1, "login.example", now.Add(time.Hour)))
	require.NoError(t, err)
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: k1, GrantId: accepted.GrantId})
	require.NoError(t, err)
	_, err = ms.CreateGrant(f.ctx, sessionKeyGrantMsg(oldAddr, k2, now.Add(time.Hour)))
	require.NoError(t, err)
	hooks = keeper.NewMigrationHooks(&f.keeper)
	require.NoError(t, hooks.AfterMemberMigration(f.ctx, oldAcc, newAcc, reptypes.MigrationPath_MIGRATION_PATH_RECOVERY))
	allowed, err = f.keeper.IsGranterAllowed(f.ctx, k1, newAddr)
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = f.keeper.IsGranterAllowed(f.ctx, k1, oldAddr)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, depositCoins(), ledger.refunded[newAddr], "pending deposit refunded to the new key")
	pendingN, err = f.keeper.CountPendingGrants(f.ctx, newAddr)
	require.NoError(t, err)
	require.Zero(t, pendingN)
	requireInvariants(t, f)
}

// The legacy MsgRevokeSession on a PENDING key refunds the consent deposit
// and clears the escrow row like MsgRevokeGrant (SES-201).
func TestPendingGrant_LegacyRevokeSessionRefunds(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("hot_key", f.addressCodec)

	resp, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, grantee, now.Add(time.Hour)))
	require.NoError(t, err)
	require.Equal(t, depositCoins(), ledger.escrowed)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithEventManager(sdk.NewEventManager())
	_, err = ms.RevokeSession(sdkCtx, &types.MsgRevokeSession{Granter: granter, Grantee: grantee})
	require.NoError(t, err)
	require.Equal(t, depositCoins(), ledger.refunded[granter], "deposit refunded to the granter")
	_, err = f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.ErrorIs(t, err, collections.ErrNotFound)
	_, err = f.keeper.PendingGrantDeposit.Get(f.ctx, resp.GrantId)
	require.ErrorIs(t, err, collections.ErrNotFound, "escrow row goes with the grant")
	pendingN, err := f.keeper.CountPendingGrants(f.ctx, granter)
	require.NoError(t, err)
	require.Zero(t, pendingN)
	var refundAttr string
	for _, ev := range sdkCtx.EventManager().Events() {
		if ev.Type != "session_revoked" {
			continue
		}
		for _, a := range ev.Attributes {
			if a.Key == "refund_amount" {
				refundAttr = a.Value
			}
		}
	}
	require.Equal(t, depositCoins()[0].String(), refundAttr, "session_revoked carries refund_amount")
	requireInvariants(t, f)

	// The same facade on an active key refunds nothing.
	ledger.refunded = map[string]sdk.Coins{}
	_, err = ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, grantee, now.Add(time.Hour)))
	require.NoError(t, err)
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: grantee, GrantId: resp.GrantId + 1})
	require.NoError(t, err)
	ledger.refunded = map[string]sdk.Coins{}
	_, err = ms.RevokeSession(f.ctx, &types.MsgRevokeSession{Granter: granter, Grantee: grantee})
	require.NoError(t, err)
	require.Empty(t, ledger.refunded[granter])
	requireInvariants(t, f)
}

// A grantee discovers the pending grants naming it through
// PendingGrantsForGrantee (SES-202); the index follows every transition
// and survives a genesis round trip.
func TestPendingGrant_GranteeDiscovery(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)
	trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	grantee := testAddr("laptop", f.addressCodec)
	g1, g2, g3, g4 := testAddr("phoenix", f.addressCodec), testAddr("aurora", f.addressCodec),
		testAddr("zenith", f.addressCodec), testAddr("nimbus", f.addressCodec)

	_, err := qs.PendingGrantsForGrantee(f.ctx, &types.QueryPendingGrantsForGranteeRequest{Grantee: "not-an-address"})
	require.Error(t, err)

	r1, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(g1, grantee, now.Add(time.Hour)))
	require.NoError(t, err)
	r2, err := ms.CreateGrant(f.ctx, signInGrant(g2, grantee, "login.example", now.Add(time.Hour)))
	require.NoError(t, err)
	r3, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(g3, grantee, now.Add(time.Hour)))
	require.NoError(t, err)
	r4, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(g4, grantee, now.Add(time.Hour)))
	require.NoError(t, err)

	ids := func() []uint64 {
		t.Helper()
		var out []uint64
		var next []byte
		for {
			resp, err := qs.PendingGrantsForGrantee(f.ctx, &types.QueryPendingGrantsForGranteeRequest{
				Grantee: grantee, Pagination: &query.PageRequest{Key: next, Limit: 2},
			})
			require.NoError(t, err)
			for _, g := range resp.Grants {
				require.Equal(t, types.GrantStatus_GRANT_STATUS_PENDING, g.Status)
				require.Equal(t, grantee, g.Grantee)
				out = append(out, g.Id)
			}
			if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
				return out
			}
			next = resp.Pagination.NextKey
		}
	}
	require.Equal(t, []uint64{r1.GrantId, r2.GrantId, r3.GrantId, r4.GrantId}, ids(), "paged over two pages")
	require.Zero(t, granteeIndexCount(t, f, grantee), "still absent from the active grantee index")
	requireInvariants(t, f)

	// Genesis rebuilds the index.
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))
	requireInvariants(t, f2)
	qs2 := keeper.NewQueryServerImpl(f2.keeper)
	listed, err := qs2.PendingGrantsForGrantee(f2.ctx, &types.QueryPendingGrantsForGranteeRequest{Grantee: grantee})
	require.NoError(t, err)
	require.Len(t, listed.Grants, 4)

	// Accept, decline, revoke and expire each drop their entry.
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: grantee, GrantId: r1.GrantId})
	require.NoError(t, err)
	_, err = ms.DeclineGrant(f.ctx, &types.MsgDeclineGrant{Grantee: grantee, GrantId: r2.GrantId})
	require.NoError(t, err)
	_, err = ms.RevokeSession(f.ctx, &types.MsgRevokeSession{Granter: g3, Grantee: grantee})
	require.NoError(t, err)
	require.Equal(t, []uint64{r4.GrantId}, ids())
	require.Equal(t, 1, granteeIndexCount(t, f, grantee), "the accepted grant moved to the active index")
	requireInvariants(t, f)
	ttl := time.Duration(types.PendingGrantTTLSeconds) * time.Second
	later := sdk.UnwrapSDKContext(withBlockTime(f.ctx, now.Add(ttl+time.Second)))
	require.NoError(t, f.keeper.EndBlocker(later))
	require.Empty(t, ids())
	requireInvariants(t, f)

	// The invariant catches a stray entry.
	require.NoError(t, f.keeper.PendingGrantsByGrantee.Set(f.ctx, collections.Join(grantee, uint64(404))))
	msg, broken := keeper.PendingGrantsInvariant(f.keeper)(sdk.UnwrapSDKContext(f.ctx))
	require.True(t, broken, msg)
}

// A module account can neither accept nor use an access-class grant, so
// naming one as grantee is refused at creation (SES-205); payout-class
// grants to module accounts are unaffected.
func TestPendingGrant_ModuleAccountGranteeRefused(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter := testAddr("granter", f.addressCodec)
	moduleAddr := authtypes.NewModuleAddress("aurora")
	f.authKeeper.GetAccountFn = func(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
		if addr.Equals(moduleAddr) {
			return authtypes.NewEmptyModuleAccount("aurora")
		}
		return nil
	}
	grantee := moduleAddr.String()

	_, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, grantee, now.Add(time.Hour)))
	require.ErrorIs(t, err, types.ErrModuleAccountGrantee)
	_, err = ms.CreateGrant(f.ctx, signInGrant(granter, grantee, "login.example", now.Add(time.Hour)))
	require.ErrorIs(t, err, types.ErrModuleAccountGrantee)
	_, err = ms.CreateSession(f.ctx, &types.MsgCreateSession{
		Granter: granter, Grantee: grantee, AllowedMsgTypes: []string{"/sparkdream.blog.v1.MsgCreatePost"},
		SpendLimit: sdk.NewInt64Coin("uspark", 1_000_000), Expiration: now.Add(time.Hour), MaxExecCount: 10,
	})
	require.ErrorIs(t, err, types.ErrModuleAccountGrantee)
	require.Empty(t, ledger.escrowed, "nothing escrowed")
	pendingN, err := f.keeper.CountPendingGrants(f.ctx, granter)
	require.NoError(t, err)
	require.Zero(t, pendingN)

	_, err = ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter: granter, Grantee: grantee, ExpiresAt: now.Add(time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{RecurringPull: &types.RecurringPullPayload{
			AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000), PeriodSeconds: 86_400,
		}},
	})
	require.NoError(t, err, "payout-class grants to a module account are still fine")
	requireInvariants(t, f)
}

// CheckSessionAcceptable (SES-203): nil for an active key or a pending key
// the first exec would accept; the acceptance error otherwise.
func TestPendingGrant_CheckSessionAcceptable(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("hot_key", f.addressCodec)

	require.ErrorIs(t, f.keeper.CheckSessionAcceptable(f.ctx, granter, grantee), types.ErrSessionNotFound)
	_, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, grantee, now.Add(time.Hour)))
	require.NoError(t, err)
	require.NoError(t, f.keeper.CheckSessionAcceptable(f.ctx, granter, grantee), "acceptance would succeed")

	// Fill the granter's active session cap with allowed grantees: now the
	// pending key cannot be accepted, and the ante refuses its exec.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxSessionsPerGranter = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	other := testAddr("other_key", f.addressCodec)
	r, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, other, now.Add(time.Hour)))
	require.NoError(t, err)
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: other, GrantId: r.GrantId})
	require.NoError(t, err)
	require.NoError(t, f.keeper.CheckSessionAcceptable(f.ctx, granter, other), "active key")
	require.ErrorIs(t, f.keeper.CheckSessionAcceptable(f.ctx, granter, grantee), types.ErrMaxSessionsExceeded)
	late := withBlockTime(f.ctx, now.Add(2*time.Hour))
	require.ErrorIs(t, f.keeper.CheckSessionAcceptable(late, granter, grantee), types.ErrGrantExpired)

	// Freeing the slot makes the pending key acceptable again.
	_, err = ms.RevokeGrant(f.ctx, &types.MsgRevokeGrant{Granter: granter, GrantId: r.GrantId})
	require.NoError(t, err)
	require.NoError(t, f.keeper.CheckSessionAcceptable(f.ctx, granter, grantee))
	requireInvariants(t, f)
}

// Grantee-side pending grants of a migrating member follow the grantee
// policy: a pending session key is dropped with its deposit refunded to its
// granter, a pending sign-in is re-keyed and stays pending.
func TestPendingGrant_MigrationGranteeSide(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	oldAddr, newAddr := testAddr("old_member", f.addressCodec), testAddr("new_member", f.addressCodec)
	stranger, friend := testAddr("stranger", f.addressCodec), testAddr("friend", f.addressCodec)
	oldAcc, err := f.addressCodec.StringToBytes(oldAddr)
	require.NoError(t, err)
	newAcc, err := f.addressCodec.StringToBytes(newAddr)
	require.NoError(t, err)

	keyResp, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(stranger, oldAddr, now.Add(time.Hour)))
	require.NoError(t, err)
	signResp, err := ms.CreateGrant(f.ctx, signInGrant(friend, oldAddr, "login.example", now.Add(time.Hour)))
	require.NoError(t, err)

	hooks := keeper.NewMigrationHooks(&f.keeper)
	require.NoError(t, hooks.AfterMemberMigration(f.ctx, oldAcc, newAcc, reptypes.MigrationPath_MIGRATION_PATH_KEY))
	_, err = f.keeper.Grants.Get(f.ctx, keyResp.GrantId)
	require.ErrorIs(t, err, collections.ErrNotFound, "pending session key dropped")
	require.Equal(t, depositCoins(), ledger.refunded[stranger], "its deposit went back to its granter")
	require.Empty(t, ledger.refunded[newAddr])
	g, err := f.keeper.Grants.Get(f.ctx, signResp.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PENDING, g.Status)
	require.Equal(t, newAddr, g.Grantee, "pending sign-in re-keyed")
	oldList, err := qs.PendingGrantsForGrantee(f.ctx, &types.QueryPendingGrantsForGranteeRequest{Grantee: oldAddr})
	require.NoError(t, err)
	require.Empty(t, oldList.Grants)
	newList, err := qs.PendingGrantsForGrantee(f.ctx, &types.QueryPendingGrantsForGranteeRequest{Grantee: newAddr})
	require.NoError(t, err)
	require.Len(t, newList.Grants, 1)
	pendingN, err := f.keeper.CountPendingGrants(f.ctx, friend)
	require.NoError(t, err)
	require.Equal(t, uint32(1), pendingN)
	requireInvariants(t, f)
	_, err = ms.AcceptGrant(f.ctx, &types.MsgAcceptGrant{Grantee: newAddr, GrantId: signResp.GrantId})
	require.NoError(t, err, "the new address accepts it")
	requireInvariants(t, f)
}

// A flood of pending grants naming a migrating member as grantee cannot
// block the migration: the hook carries over at most
// MaxMigratedPendingGrants and leaves the rest, inert, under the old address.
func TestPendingGrant_MigrationGranteeSideCapped(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	oldAddr, newAddr := testAddr("old_member", f.addressCodec), testAddr("new_member", f.addressCodec)
	oldAcc, err := f.addressCodec.StringToBytes(oldAddr)
	require.NoError(t, err)
	newAcc, err := f.addressCodec.StringToBytes(newAddr)
	require.NoError(t, err)

	total := types.MaxMigratedPendingGrants + 5
	for i := 0; i < total; i++ {
		granter := testAddr(fmt.Sprintf("flood_%d", i), f.addressCodec)
		_, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, oldAddr, now.Add(time.Hour)))
		require.NoError(t, err)
	}

	hooks := keeper.NewMigrationHooks(&f.keeper)
	require.NoError(t, hooks.AfterMemberMigration(f.ctx, oldAcc, newAcc, reptypes.MigrationPath_MIGRATION_PATH_KEY))

	left := 0
	rng := collections.NewPrefixedPairRange[string, uint64](oldAddr)
	require.NoError(t, f.keeper.PendingGrantsByGrantee.Walk(f.ctx, rng, func(collections.Pair[string, uint64]) (bool, error) {
		left++
		return false, nil
	}))
	require.Equal(t, total-types.MaxMigratedPendingGrants, left, "grants past the cap stay under the old address")
	requireInvariants(t, f)
}

// A pending grant whose deposit can be moved nowhere is left as it was by
// the expiry pass, deposit row included, and expires once the bank recovers:
// the escrow row never stops matching the coins the module holds.
func TestPendingGrant_ExpiryFailedForfeitChangesNothing(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	ledger := trackDeposits(f)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	granter := testAddr("granter", f.addressCodec)
	ttl := time.Duration(types.PendingGrantTTLSeconds) * time.Second
	resp, err := ms.CreateGrant(f.ctx, sessionKeyGrantMsg(granter, testAddr("aurora", f.addressCodec), now.Add(2*ttl)))
	require.NoError(t, err)

	ledger.poolError = errors.New("pool down")
	f.bankKeeper.SendCoinsFromModuleToModuleFn = func(context.Context, string, string, sdk.Coins) error {
		return errors.New("fee collector down")
	}
	after := sdk.UnwrapSDKContext(withBlockTime(f.ctx, now.Add(ttl+time.Minute)))
	require.NoError(t, f.keeper.EndBlocker(after))
	g, err := f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PENDING, g.Status)
	has, err := f.keeper.PendingGrantDeposit.Has(f.ctx, resp.GrantId)
	require.NoError(t, err)
	require.True(t, has, "the deposit row stays with the coins")

	ledger.poolError = nil
	require.NoError(t, f.keeper.EndBlocker(after))
	_, err = f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.ErrorIs(t, err, collections.ErrNotFound)
	require.Equal(t, depositCoins(), ledger.pool)
	requireInvariants(t, f)
}
