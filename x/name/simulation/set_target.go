package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgSetTarget has a name's owner point it at another live sim
// account (one draw in eight: clear the target). A member without a name
// registers one first.
func SimulateMsgSetTarget(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSetTarget{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg, owner, reason, err := e.newSetTarget(e.r.Intn(8) == 0)
		if err != nil {
			return noOpErr(label, err)
		}
		if reason != "" {
			return noOp(label, reason)
		}
		return e.submit(owner, msg, zero())
	})
}

// newSetTarget builds a MsgSetTarget from the owner of a live name to a
// different live sim account (or clearing it).
func (e *simEnv) newSetTarget(clear bool) (*types.MsgSetTarget, simtypes.Account, string, error) {
	rec, owner, ok, err := e.ownedName(anyone, nil)
	if err != nil || !ok {
		return nil, simtypes.Account{}, "no live sim account owns a name and none can register one", err
	}
	target := ""
	if !clear {
		others := e.live(func(a simtypes.Account) bool {
			return a.Address.String() != rec.Target && !e.sameMember(a.Address.String(), owner.Address.String())
		})
		acc, ok := pick(e.r, others)
		if !ok {
			return nil, simtypes.Account{}, "no other live sim account to target", nil
		}
		target = acc.Address.String()
	}
	return &types.MsgSetTarget{Authority: owner.Address.String(), Name: rec.Name, Target: target}, owner, "", nil
}

// SimulateMsgAcceptTarget has a live sim account accept a name pointed at
// it. When no name awaits acceptance, an owner points one at a live sim
// account first (a real MsgSetTarget).
func SimulateMsgAcceptTarget(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgAcceptTarget{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		pending, err := e.namesWhere(func(rec types.NameRecord) bool {
			_, ok := e.account(rec.Target)
			return rec.Target != "" && !rec.TargetAccepted && ok
		})
		if err != nil {
			return noOpErr(label, err)
		}
		rec, ok := pick(e.r, pending)
		if !ok {
			set, owner, reason, err := e.newSetTarget(false)
			if err != nil {
				return noOpErr(label, err)
			}
			if reason != "" {
				return noOp(label, reason)
			}
			if ok, err := e.ensure(owner, set, zero()); err != nil || !ok {
				if err != nil {
					return noOpErr(label, err)
				}
				return noOp(label, "could not point a name at a sim account")
			}
			found := false
			if rec, found, err = e.name(set.Name); err != nil {
				return noOpErr(label, err)
			}
			if !found {
				return noOp(label, "the targeted name vanished")
			}
		}
		target, ok := e.account(rec.Target)
		if !ok {
			return noOp(label, "the target is not a live sim account")
		}
		return e.submit(target, &types.MsgAcceptTarget{Authority: rec.Target, Name: rec.Name}, zero())
	})
}
