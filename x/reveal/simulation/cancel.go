package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// cancelMinAge is how many blocks a contribution stays untouched by the
// contributor-cancel op, so most contributions get through approval and
// staking first.
const cancelMinAge = 20

// SimulateMsgCancel has a contributor cancel its own contribution while it
// is PROPOSED or IN_PROGRESS and no tranche has reached BACKED (the
// handler's contributor path). A contribution an open council proposal
// targets is skipped, so the proposal does not go stale.
//
// The other path (governance, the Commons Council policy or the Commons
// Operations Committee policy cancelling any contribution) is signed only by
// a policy address; sim accounts are never a policy, so it is not
// simulated.
func SimulateMsgCancel(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgCancel{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		props, err := e.revealProposals()
		if err != nil {
			return noOpErr(&types.MsgCancel{}, err)
		}
		taken := covered(props)
		cancellable, err := e.contributionsWhere(func(c types.Contribution) bool {
			if c.Status != types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED &&
				c.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
				return false
			}
			if taken[c.Id] || e.height()-c.CreatedAt < cancelMinAge {
				return false
			}
			if keeper.HasAnyTrancheReachedStatus(&c, types.TrancheStatus_TRANCHE_STATUS_BACKED) {
				return false
			}
			_, ok := e.account(c.Contributor)
			return ok
		})
		if err != nil {
			return noOpErr(&types.MsgCancel{}, err)
		}
		c, ok := pick(e.r, cancellable)
		if !ok {
			return noOp(&types.MsgCancel{}, "no contribution its contributor may cancel")
		}
		acc, ok := e.account(c.Contributor)
		if !ok {
			return noOp(&types.MsgCancel{}, "contributor is not a live sim account")
		}
		return e.submit(acc, &types.MsgCancel{Authority: acc.Address.String(), ContributionId: c.Id, Reason: randomReason(e.r)})
	})
}
