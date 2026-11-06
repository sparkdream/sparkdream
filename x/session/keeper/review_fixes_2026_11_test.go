package keeper_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	blogtypes "sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// granterRouter records the session-exec granter each inner handler sees.
type granterRouter struct{ seen *[]string }

func (r granterRouter) Handler(sdk.Msg) baseAppMsgServiceHandler { return r.handle }
func (r granterRouter) HandlerByTypeURL(string) baseAppMsgServiceHandler {
	return r.handle
}
func (r granterRouter) handle(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
	*r.seen = append(*r.seen, commontypes.SessionExecGranter(ctx))
	return &sdk.Result{}, nil
}

// SES-101: every inner message of MsgExecSession runs with the granter in
// commontypes.ContextKeySessionExec (x/artifact refunds session-paid mint
// deposits to it); the outer context is left untouched.
func TestSessionExecTagsInnerContext(t *testing.T) {
	f := initFixture(t)
	var seen []string
	f.keeper.SetRouter(granterRouter{seen: &seen})
	ms := keeper.NewMsgServerImpl(f.keeper)
	granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
	createTestSessionWithExec(t, f, granter, grantee, []string{"/sparkdream.blog.v1.MsgCreatePost"},
		sdk.UnwrapSDKContext(f.ctx).BlockTime().Add(time.Hour), 5)
	post, err := codectypes.NewAnyWithValue(&blogtypes.MsgCreatePost{Creator: grantee})
	require.NoError(t, err)
	_, err = ms.ExecSession(f.ctx, &types.MsgExecSession{Granter: granter, Grantee: grantee, Msgs: []*codectypes.Any{post, post}})
	require.NoError(t, err)
	require.Equal(t, []string{granter, granter}, seen)
	require.Empty(t, commontypes.SessionExecGranter(sdk.UnwrapSDKContext(f.ctx)))
}

// SES-102: the grantee cap is per class, so free payout grants cannot
// block the session keys and sign-ins a grantee needs, and the legacy
// MsgCreateSession path enforces the cap too.
func TestGranteeCapPerClassAndLegacyPath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime()
	daemon := testAddr("daemon", f.addressCodec)
	sybil := func(i int) string { return testAddr(fmt.Sprintf("sybil%03d", i), f.addressCodec) }

	// Fill the payout class with RecurringPulls (no escrow, so the cheapest
	// filler there is).
	pull := func(i int) error {
		_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
			Granter: sybil(i), Grantee: daemon, ExpiresAt: now.Add(72 * time.Hour),
			Payload: &types.MsgCreateGrant_RecurringPull{RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000), PeriodSeconds: 86_400,
			}},
		})
		return err
	}
	for i := 0; i < keeper.MaxGrantsPerGrantee; i++ {
		require.NoError(t, pull(i), "pull %d", i)
	}
	require.ErrorIs(t, pull(keeper.MaxGrantsPerGrantee), types.ErrTooManyGrantsForGrantee)

	// The operator (allowed by the daemon) still renews the daemon's
	// session key.
	operator := testAddr("operator", f.addressCodec)
	allowGranter(t, f, daemon, operator)
	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter: operator, Grantee: daemon, ExpiresAt: now.Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_SessionKey{SessionKey: &types.SessionKeyPayload{
			AllowedMsgTypes: []string{"/sparkdream.service.v1.MsgSubmitCheckpoint"},
			SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000), MaxExecCount: 10,
		}},
	})
	require.NoError(t, err, "payout grants do not consume the access class")

	// The typed listing walks only the requested type.
	pulls, err := f.keeper.ListGrantsByGrantee(f.ctx, daemon, types.GrantType_GRANT_TYPE_RECURRING_PULL)
	require.NoError(t, err)
	require.Len(t, pulls, keeper.MaxGrantsPerGrantee)
	all, err := f.keeper.ListGrantsByGrantee(f.ctx, daemon, types.GrantType_GRANT_TYPE_UNSPECIFIED)
	require.NoError(t, err)
	require.Len(t, all, keeper.MaxGrantsPerGrantee+1)

	// Legacy MsgCreateSession counts against the access class once the
	// grantee allows the granter (an unknown granter's key is PENDING and
	// never reaches the class, SES-102).
	victim := testAddr("victim", f.addressCodec)
	legacy := func(i int) error {
		allowGranter(t, f, victim, sybil(i))
		_, err := ms.CreateSession(f.ctx, &types.MsgCreateSession{
			Granter: sybil(i), Grantee: victim, AllowedMsgTypes: []string{"/sparkdream.blog.v1.MsgCreatePost"},
			SpendLimit: sdk.NewInt64Coin("uspark", 1_000_000), Expiration: now.Add(time.Hour), MaxExecCount: 10,
		})
		return err
	}
	for i := 0; i < keeper.MaxGrantsPerGrantee; i++ {
		require.NoError(t, legacy(i), "session %d", i)
	}
	require.ErrorIs(t, legacy(keeper.MaxGrantsPerGrantee), types.ErrTooManyGrantsForGrantee)

	msg, broken := keeper.GrantIndexesInvariant(f.keeper)(sdk.UnwrapSDKContext(f.ctx))
	require.False(t, broken, msg)
}

// SES-104: a grantee retry that pauses again keeps the first pause time,
// so the paused-TTL auto-revoke still comes due; a granter retry restarts
// the clock.
func TestGranteeRetryKeepsPausedTTL(t *testing.T) {
	for _, byGranter := range []bool{false, true} {
		t.Run(fmt.Sprintf("granter=%t", byGranter), func(t *testing.T) {
			f := initFixture(t)
			ms := keeper.NewMsgServerImpl(f.keeper)
			start := sdk.UnwrapSDKContext(f.ctx).BlockTime()
			params, err := f.keeper.Params.Get(f.ctx)
			require.NoError(t, err)
			ttl := time.Duration(params.PausedOneshotTtlSeconds) * time.Second

			granter, grantee := testAddr("granter", f.addressCodec), testAddr("grantee", f.addressCodec)
			fireAt := start.Add(time.Hour)
			id, _ := createTransferOneshot(t, f, ms, granter, grantee, testAddr("recipient", f.addressCodec),
				sdk.NewInt64Coin("uspark", 5_000_000), fireAt.Unix(), start.Add(300*24*time.Hour))
			f.bankKeeper.SendCoinsFn = underfundedSend
			f.bankKeeper.SendCoinsFromModuleToAccountFn = func(context.Context, string, sdk.AccAddress, sdk.Coins) error { return nil }
			require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(withBlockTime(f.ctx, fireAt))))

			// Retry just before the TTL; the fire pauses it again.
			retryAt := sdk.UnwrapSDKContext(withBlockTime(f.ctx, fireAt.Add(ttl-time.Minute)))
			caller := grantee
			if byGranter {
				caller = granter
			}
			_, err = ms.RetryScheduledOneshot(retryAt, &types.MsgRetryScheduledOneshot{Caller: caller, GrantId: id})
			require.NoError(t, err)
			require.NoError(t, f.keeper.EndBlocker(retryAt))
			g, err := f.keeper.Grants.Get(retryAt, id)
			require.NoError(t, err)
			require.Equal(t, types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS, g.Status)

			// At the original TTL the grantee-retried grant is revoked; the
			// granter-retried one counts from its new pause.
			due := sdk.UnwrapSDKContext(withBlockTime(f.ctx, fireAt.Add(ttl)))
			require.NoError(t, f.keeper.EndBlocker(due))
			_, err = f.keeper.Grants.Get(due, id)
			if byGranter {
				require.NoError(t, err)
				later := sdk.UnwrapSDKContext(withBlockTime(f.ctx, retryAt.BlockTime().Add(ttl)))
				require.NoError(t, f.keeper.EndBlocker(later))
				_, err = f.keeper.Grants.Get(later, id)
			}
			require.ErrorIs(t, err, collections.ErrNotFound)
		})
	}
}
