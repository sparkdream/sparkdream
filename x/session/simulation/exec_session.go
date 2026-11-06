package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// execFeeCap bounds the fee of a MsgExecSession (0.02 SPARK). The
// SessionFeeDecorator takes it from the granter and debits the session's
// spend_limit budget, so it must fit what is left of both.
const execFeeCap = 20_000

// buildExec builds a MsgExecSession through session key g carrying one
// inner message of a type the key allows: a buildable allowlisted message,
// or (allow_self_revoke) a MsgRevokeGrant of another of the granter's
// grants. It returns the message and the fee the granter pays; reason is
// non-empty when the key cannot execute anything now.
func (e *simEnv) buildExec(g types.Grant) (*types.MsgExecSession, sdk.Coins, string, error) {
	sk := g.GetSessionKey()
	now := e.ctx.BlockTime()
	switch {
	case sk == nil || g.Status != types.GrantStatus_GRANT_STATUS_ACTIVE:
		return nil, nil, "session key is not active", nil
	case !g.ExpiresAt.After(now):
		return nil, nil, "session key expired", nil
	case sk.MaxExecCount > 0 && sk.ExecCount+1 > sk.MaxExecCount:
		return nil, nil, "session key used up its exec count", nil
	case !sk.SpendLimit.IsPositive() || !sk.Spent.Amount.LT(sk.SpendLimit.Amount):
		return nil, nil, "session key used up its fee budget", nil
	}

	var inners []sdk.Msg
	var revoke sdk.Msg
	for _, t := range sk.AllowedMsgTypes {
		if t == types.MsgRevokeGrantTypeURL {
			if !sk.AllowSelfRevoke {
				continue
			}
			// Never the executing key itself: see the keeper-bug note in
			// the simulation report (ExecSession rewrites its own grant
			// after the inner revoke deleted it).
			others, err := e.k.ListGrantsByGranter(e.ctx, g.Granter, types.GrantType_GRANT_TYPE_UNSPECIFIED)
			if err != nil {
				return nil, nil, "", err
			}
			for _, i := range e.r.Perm(len(others)) {
				if others[i].Id != g.Id && actionable(others[i]) {
					revoke = &types.MsgRevokeGrant{Granter: g.Granter, GrantId: others[i].Id}
					inners = append(inners, revoke)
					break
				}
			}
			continue
		}
		build, ok := innerBuilders[t]
		if !ok || !e.globallyAllowed(t) {
			continue
		}
		inners = append(inners, build(e, g.Granter))
	}
	inner, ok := pick(e.r, inners)
	// A self-revoke key revokes one of the granter's grants half the time.
	if revoke != nil && e.r.Intn(2) == 0 {
		inner, ok = revoke, true
	}
	if !ok {
		return nil, nil, "session key allows no message the sim can build", nil
	}
	anyMsg, err := codectypes.NewAnyWithValue(inner)
	if err != nil {
		return nil, nil, "", err
	}
	msg := &types.MsgExecSession{Grantee: g.Grantee, Granter: g.Granter, Msgs: []*codectypes.Any{anyMsg}}

	// The fee comes out of the granter and the session budget.
	feeCap := math.MinInt(sk.SpendLimit.Amount.Sub(sk.Spent.Amount), math.NewInt(execFeeCap))
	feeCap = math.MinInt(feeCap, e.spendableOf(g.Granter).QuoRaw(10))
	fee := sdk.NewCoins()
	if feeCap.IsPositive() && e.r.Intn(5) != 0 {
		amt, err := simtypes.RandPositiveInt(e.r, feeCap)
		if err == nil {
			fee = sdk.NewCoins(sdk.NewCoin(e.denom(), amt))
		}
	}
	return msg, fee, "", nil
}

// ensureRevokeTarget gives an allow_self_revoke key's granter, half the
// time it has no other pending grant, one to revoke: a real MsgCreateGrant
// (a sign-in grant, which escrows nothing) from the granter.
func (e *simEnv) ensureRevokeTarget(g types.Grant, granter simtypes.Account) error {
	sk := g.GetSessionKey()
	if sk == nil || !sk.AllowSelfRevoke || e.r.Intn(2) == 0 {
		return nil
	}
	others, err := e.k.ListGrantsByGranter(e.ctx, g.Granter, types.GrantType_GRANT_TYPE_UNSPECIFIED)
	if err != nil {
		return err
	}
	for _, o := range others {
		if o.Id != g.Id && actionable(o) {
			return nil
		}
	}
	grantee, ok := pick(e.r, e.live(func(a simtypes.Account) bool { return !a.Address.Equals(granter.Address) }))
	if !ok {
		return nil
	}
	msg, spent, reason, err := e.buildGrant(kindSignIn, granter, grantee)
	if err != nil || reason != "" {
		return err
	}
	_, _, err = e.ensure(granter, msg, spent)
	return err
}

// execute dry-runs and delivers an exec through session key g, signed by
// its grantee with the fee charged to the granter.
func (e *simEnv) execute(g types.Grant) (simtypes.OperationMsg, bool, string, error) {
	grantee, ok := e.account(g.Grantee)
	if !ok {
		return simtypes.OperationMsg{}, false, "grantee is not a live sim account", nil
	}
	granter, ok := e.account(g.Granter)
	if !ok {
		return simtypes.OperationMsg{}, false, "granter is not a live sim account", nil
	}
	if err := e.ensureRevokeTarget(g, granter); err != nil {
		return simtypes.OperationMsg{}, false, "", err
	}
	msg, fee, reason, err := e.buildExec(g)
	if err != nil || reason != "" {
		return simtypes.OperationMsg{}, false, reason, err
	}
	granterAddr, err := sdk.AccAddressFromBech32(g.Granter)
	if err != nil {
		return simtypes.OperationMsg{}, false, "", err
	}
	if err := e.dryRunAfterFee(msg, granterAddr, fee); err != nil {
		return simtypes.OperationMsg{}, false, "handler would reject: " + err.Error(), nil
	}
	opMsg, _, err := simulation.GenAndDeliverTx(e.input(grantee, msg, math.ZeroInt()), fee)
	e.invalidate()
	return opMsg, true, "", err
}

// SimulateMsgExecSession executes an allowlisted inner message through a
// session key: the grantee signs, the SessionFeeDecorator charges the fee
// to the granter, and the inner message runs as the granter. When no live
// session key can execute anything, the op first creates one with a real
// MsgCreateGrant and executes through it.
func SimulateMsgExecSession(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgExecSession{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		sessions, err := e.grants(func(g types.Grant) bool {
			return g.Type == types.GrantType_GRANT_TYPE_SESSION_KEY && g.Status == types.GrantStatus_GRANT_STATUS_ACTIVE
		})
		if err != nil {
			return noOpErr(label, err)
		}
		reason := "no session key"
		for _, i := range e.r.Perm(len(sessions)) {
			opMsg, done, why, err := e.execute(sessions[i])
			if err != nil || done {
				return opMsg, nil, err
			}
			reason = why
		}

		// Prerequisite: a fresh session key.
		created, g, ok, err := e.createGrant(kindSessionKey)
		if err != nil {
			return created, nil, err
		}
		if !ok {
			return noOp(label, reason+"; no session key could be created")
		}
		opMsg, done, why, err := e.execute(g)
		if err != nil || done {
			return opMsg, nil, err
		}
		// The create was delivered; report it rather than a NoOp.
		created.Comment = "created a session key that cannot execute yet: " + why
		return created, nil, nil
	})
}
