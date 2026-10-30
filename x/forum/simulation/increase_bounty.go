package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgIncreaseBounty delivers MsgIncreaseBounty from the creator of
// an ACTIVE bounty, escrowing at least the minimum increase. When no sim
// account holds an active bounty, one is first opened (a real
// MsgCreateBounty).
func SimulateMsgIncreaseBounty(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgIncreaseBounty{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgIncreaseBounty{}
		if e.params.ForumPaused {
			return noOp(msg, "forum is paused")
		}
		bounties, err := e.activeSimBounties(func(b types.Bounty) bool {
			creator, ok := e.account(b.Creator)
			return ok && e.spendable(creator).GT(math.NewInt(types.MinBountyAmount))
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		bounty, ok := pick(e.r, bounties)
		if !ok {
			return noOp(msg, "no active bounty held by a sim account")
		}
		creator, _ := e.account(bounty.Creator)
		amount := math.NewInt(types.MinBountyAmount)
		msg.Creator, msg.BountyId, msg.AdditionalAmount = bounty.Creator, bounty.Id, amount.String()
		return e.submit(creator, msg, amount)
	})
}

// activeSimBounties returns the ACTIVE bounties whose creator is a live sim
// account and that keep accepts (nil accepts all). When there is none, one
// is first opened (a real MsgCreateBounty).
func (e *simEnv) activeSimBounties(keep func(types.Bounty) bool) ([]types.Bounty, error) {
	list := func() ([]types.Bounty, error) {
		return e.bountiesWhere(func(b types.Bounty) bool {
			if b.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE || (keep != nil && !keep(b)) {
				return false
			}
			_, ok := e.account(b.Creator)
			return ok
		})
	}
	out, err := list()
	if err != nil || len(out) > 0 {
		return out, err
	}
	if ok, err := e.ensureBounty(); err != nil || !ok {
		return nil, err
	}
	return list()
}
