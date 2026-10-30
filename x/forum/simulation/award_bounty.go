package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgAwardBounty delivers MsgAwardBounty from the creator of an
// ACTIVE bounty with at least one assigned reply that is still ACTIVE (the
// escrow is split among those). When no sim account holds such a bounty, a
// reply is first assigned (a real MsgAssignBountyToReply, building the
// bounty and reply first when needed).
func SimulateMsgAwardBounty(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAwardBounty{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAwardBounty{}
		awardable := func(b types.Bounty) bool {
			for _, a := range b.Awards {
				if p, ok := e.post(a.PostId); ok && p.Status == types.PostStatus_POST_STATUS_ACTIVE {
					return true
				}
			}
			return false
		}
		list := func() ([]types.Bounty, error) {
			return e.bountiesWhere(func(b types.Bounty) bool {
				_, ok := e.account(b.Creator)
				return ok && b.Status == types.BountyStatus_BOUNTY_STATUS_ACTIVE && awardable(b)
			})
		}
		bounties, err := list()
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(bounties) == 0 {
			assignable, err := e.assignableBounties()
			if err != nil {
				return noOpErr(msg, err)
			}
			if b, ok := pick(e.r, assignable); ok {
				assign, reason, err := e.newAssignMsg(b)
				if err != nil {
					return noOpErr(msg, err)
				}
				if reason == "" {
					creator, _ := e.account(b.Creator)
					if _, err := e.ensure(creator, assign, math.ZeroInt()); err != nil {
						return noOpErr(msg, err)
					}
				}
			}
			if bounties, err = list(); err != nil {
				return noOpErr(msg, err)
			}
		}
		bounty, ok := pick(e.r, bounties)
		if !ok {
			return noOp(msg, "no active bounty with an eligible assigned reply")
		}
		creator, _ := e.account(bounty.Creator)
		msg.Creator, msg.BountyId = bounty.Creator, bounty.Id
		return e.submit(creator, msg, math.ZeroInt())
	})
}
