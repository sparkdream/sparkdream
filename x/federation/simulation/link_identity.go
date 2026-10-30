package simulation

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgLinkIdentity delivers MsgLinkIdentity: a live sim account
// claims a "@name@<peer>" handle on an ACTIVE external-protocol peer where
// it has no link yet and is under max_identity_links_per_user. Spark Dream
// peers are skipped: their link sends an IBC challenge packet and the
// simulation has no IBC channel.
func SimulateMsgLinkIdentity(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgLinkIdentity{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		acc, msg, reason, err := e.linkMsg(anyone)
		if err != nil {
			return noOpErr(msg, err)
		}
		if reason != "" {
			return noOp(msg, reason)
		}
		return e.submit(acc, msg, math.ZeroInt())
	})
}

// linkMsg builds a MsgLinkIdentity for a live sim account accepted by keep
// on an ACTIVE external-protocol peer where it has no link yet, under the
// per-user link cap, claiming a fresh "@name@<peer>" handle. A non-empty
// reason means no such link can be made now.
func (e *simEnv) linkMsg(keep func(simtypes.Account) bool) (simtypes.Account, *types.MsgLinkIdentity, string, error) {
	msg := &types.MsgLinkIdentity{}
	peers, err := e.peersWhere(activeBridged)
	if err != nil {
		return simtypes.Account{}, msg, "", err
	}
	p, ok := pick(e.r, peers)
	if !ok {
		return simtypes.Account{}, msg, "no ACTIVE external-protocol peer (activation is policy-only)", nil
	}
	acc, ok := pick(e.r, accountsWhere(e.live(), func(a simtypes.Account) bool {
		addr := a.Address.String()
		has, err := e.k.IdentityLinks.Has(e.ctx, collections.Join(addr, p.Id))
		return err == nil && !has && keep(a) && e.linkCount(addr) < e.params.MaxIdentityLinksPerUser
	}))
	if !ok {
		return simtypes.Account{}, msg, "no account without a link on the peer and under the link cap", nil
	}
	remote := fmt.Sprintf("@%s%d@%s", randomCreatorName(e.r), e.r.Intn(1_000_000), p.Id)
	if has, err := e.k.IdentityLinksByRemote.Has(e.ctx, collections.Join(p.Id, remote)); err != nil || has {
		return simtypes.Account{}, msg, "remote identity already verified", err
	}
	msg.Creator, msg.PeerId, msg.RemoteIdentity = acc.Address.String(), p.Id, remote
	return acc, msg, "", nil
}

// ensureLink returns an identity link accepted by keep, linking one first
// through MsgLinkIdentity (from an account accepted by accountOK) when none
// exists. Unverified links expire after unverified_link_ttl, which the
// simulated clock passes quickly, so most ops need a fresh one.
func (e *simEnv) ensureLink(keep func(types.IdentityLink) bool, accountOK func(simtypes.Account) bool) (types.IdentityLink, bool, error) {
	links, err := e.linksWhere(keep)
	if err != nil {
		return types.IdentityLink{}, false, err
	}
	if l, ok := pick(e.r, links); ok {
		return l, true, nil
	}
	acc, msg, reason, err := e.linkMsg(accountOK)
	if err != nil || reason != "" {
		return types.IdentityLink{}, false, err
	}
	if ok, err := e.ensure(acc, msg, math.ZeroInt()); err != nil || !ok {
		return types.IdentityLink{}, false, err
	}
	l, err := e.k.IdentityLinks.Get(e.ctx, collections.Join(msg.Creator, msg.PeerId))
	if err != nil || !keep(l) {
		return types.IdentityLink{}, false, nil
	}
	return l, true, nil
}
