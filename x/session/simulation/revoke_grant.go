package simulation

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// selfRevoker returns a live sim grantee of an active, unexpired session key
// of g's granter that carries allow_self_revoke (and is not g itself): such
// a key may revoke the granter's other grants directly.
func (e *simEnv) selfRevoker(g types.Grant) (simtypes.Account, bool, error) {
	keys, err := e.k.ListGrantsByGranter(e.ctx, g.Granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
	if err != nil {
		return simtypes.Account{}, false, err
	}
	for _, i := range e.r.Perm(len(keys)) {
		sk := keys[i].GetSessionKey()
		if keys[i].Id == g.Id || sk == nil || !sk.AllowSelfRevoke ||
			keys[i].Status != types.GrantStatus_GRANT_STATUS_ACTIVE || !keys[i].ExpiresAt.After(e.ctx.BlockTime()) {
			continue
		}
		if has, err := e.k.SessionKeyByPair.Has(e.ctx, collections.Join(g.Granter, keys[i].Grantee)); err != nil || !has {
			continue
		}
		if acc, ok := e.account(keys[i].Grantee); ok {
			return acc, true, nil
		}
	}
	return simtypes.Account{}, false, nil
}

// SimulateMsgRevokeGrant revokes a pending grant of any type, signed by its
// live sim granter or (one call in four, when one exists) by the grantee of
// the granter's allow_self_revoke session key. A revoked oneshot refunds its
// gas deposit. When no grant has a live sim granter, the op first creates
// one with a real MsgCreateGrant and revokes it.
func SimulateMsgRevokeGrant(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgRevokeGrant{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		all, err := e.grants(actionable)
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(all)) {
			g := all[i]
			signer, ok := e.account(g.Granter)
			if !ok {
				continue
			}
			if e.r.Intn(4) == 0 {
				acc, found, err := e.selfRevoker(g)
				if err != nil {
					return noOpErr(label, err)
				}
				if found {
					signer = acc
				}
			}
			msg := &types.MsgRevokeGrant{Granter: signer.Address.String(), GrantId: g.Id}
			if e.dryRun(msg) != nil {
				continue
			}
			return e.deliver(signer, msg, math.ZeroInt())
		}

		// Prerequisite: a grant to revoke.
		created, g, ok, err := e.createGrant(grantKind(e.r.Intn(int(numGrantKinds))))
		if err != nil {
			return created, nil, err
		}
		if !ok {
			return noOp(label, "no grant with a live sim granter and none could be created")
		}
		granter, live := e.account(g.Granter)
		if !live {
			return created, nil, nil
		}
		return e.submit(granter, &types.MsgRevokeGrant{Granter: g.Granter, GrantId: g.Id}, math.ZeroInt())
	})
}
