package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgContestDispute has the owner of a disputed name contest the
// dispute inside its contest window, locking the contest stake. When no
// dispute can be contested, a member disputes a name of an owner who can
// stake first (a real MsgFileDispute).
func SimulateMsgContestDispute(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgContestDispute{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		stake := e.params.ContestStakeDream
		canContest := func(a simtypes.Account) bool {
			return e.spendable(a).IsPositive() && e.canStake(a.Address.String(), stake)
		}
		height := e.ctx.BlockHeight()
		open, err := e.activeDisputes(func(d types.Dispute) bool {
			return d.ContestChallengeId == "" && height <= e.contestDeadline(d)
		})
		if err != nil {
			return noOpErr(label, err)
		}
		type candidate struct {
			name  string
			owner simtypes.Account
		}
		var cands []candidate
		for _, d := range open {
			rec, found, err := e.name(d.Name)
			if err != nil {
				return noOpErr(label, err)
			}
			if !found {
				continue
			}
			if acc, ok := e.account(rec.Owner); ok && canContest(acc) {
				cands = append(cands, candidate{name: d.Name, owner: acc})
			}
		}
		c, ok := pick(e.r, cands)
		if !ok {
			file, claimant, reason, err := e.newFileDispute(canContest)
			if err != nil {
				return noOpErr(label, err)
			}
			if reason != "" {
				return noOp(label, "no contestable dispute: "+reason)
			}
			if ok, err := e.ensure(claimant, file, zero()); err != nil || !ok {
				if err != nil {
					return noOpErr(label, err)
				}
				return noOp(label, "no contestable dispute and none could be filed")
			}
			rec, found, err := e.name(file.Name)
			if err != nil {
				return noOpErr(label, err)
			}
			owner, live := e.account(rec.Owner)
			if !found || !live {
				return noOp(label, "the disputed name has no live owner")
			}
			c = candidate{name: file.Name, owner: owner}
		}
		msg := &types.MsgContestDispute{Authority: c.owner.Address.String(), Name: c.name, Reason: randomReason(e.r)}
		return e.submit(c.owner, msg, zero())
	})
}
