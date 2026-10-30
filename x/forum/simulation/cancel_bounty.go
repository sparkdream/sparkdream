package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgCancelBounty delivers MsgCancelBounty from the creator of an
// ACTIVE bounty with no assigned awards (the refund less the cancellation
// fee comes back from the module account). When no sim account holds such a
// bounty, one is first opened (a real MsgCreateBounty).
func SimulateMsgCancelBounty(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgCancelBounty{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCancelBounty{}
		bounties, err := e.activeSimBounties(func(b types.Bounty) bool { return len(b.Awards) == 0 })
		if err != nil {
			return noOpErr(msg, err)
		}
		bounty, ok := pick(e.r, bounties)
		if !ok {
			return noOp(msg, "no unassigned active bounty held by a sim account")
		}
		creator, _ := e.account(bounty.Creator)
		msg.Creator, msg.BountyId = bounty.Creator, bounty.Id
		return e.submit(creator, msg, math.ZeroInt())
	})
}
