package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgFileDispute has an active member with the dispute stake in
// unlocked DREAM dispute a name someone else owns and nobody disputes. When
// no such name exists another member registers one first.
func SimulateMsgFileDispute(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgFileDispute{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg, claimant, reason, err := e.newFileDispute(anyone)
		if err != nil {
			return noOpErr(label, err)
		}
		if reason != "" {
			return noOp(label, reason)
		}
		return e.submit(claimant, msg, zero())
	})
}

// newFileDispute builds a MsgFileDispute from a claimant who can stake,
// against an undisputed name whose owner keepOwner accepts (a live sim
// account), registering one first when none exists.
func (e *simEnv) newFileDispute(keepOwner func(simtypes.Account) bool) (*types.MsgFileDispute, simtypes.Account, string, error) {
	stake := e.params.DisputeStakeDream
	claimants := e.live(func(a simtypes.Account) bool {
		return e.spendable(a).IsPositive() && e.canStake(a.Address.String(), stake)
	})
	claimant, ok := pick(e.r, claimants)
	if !ok {
		return nil, simtypes.Account{}, "no live member holds the dispute stake in unlocked DREAM", nil
	}
	c := claimant.Address.String()
	var disputeErr error
	undisputed := func(rec types.NameRecord) bool {
		d, err := e.activeDispute(rec.Name)
		if err != nil {
			disputeErr = err
		}
		return err == nil && !d
	}
	rec, _, ok, err := e.ownedName(func(a simtypes.Account) bool {
		return !e.sameMember(a.Address.String(), c) && keepOwner(a)
	}, undisputed)
	if err == nil {
		err = disputeErr
	}
	if err != nil || !ok {
		return nil, simtypes.Account{}, "no undisputed name of another live account and none could be registered", err
	}
	return &types.MsgFileDispute{Authority: c, Name: rec.Name, Reason: randomReason(e.r)}, claimant, "", nil
}
