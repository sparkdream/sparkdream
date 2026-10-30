package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgUnhideReply delivers MsgUnhideReply for a random hidden reply,
// signed by the post's author (by alias). With none, the post author hides
// an active reply first (a real MsgHideReply) and then unhides it.
func SimulateMsgUnhideReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnhideReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgUnhideReply{}
		withOwner := func(status types.ReplyStatus) func(types.Reply) bool {
			return func(rp types.Reply) bool {
				_, ok := e.postOwner(rp)
				return rp.Status == status && ok
			}
		}
		rp, ok := pick(e.r, e.repliesWhere(withOwner(types.ReplyStatus_REPLY_STATUS_HIDDEN)))
		if !ok {
			var err error
			rp, ok, err = e.replyOrCreate(withOwner(types.ReplyStatus_REPLY_STATUS_ACTIVE), anyone)
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok {
				return noOp(label, "no hidden or active reply on a post with a live author")
			}
			acc, _ := e.postOwner(rp)
			hid, err := e.ensure(acc, &types.MsgHideReply{Creator: acc.Address.String(), Id: rp.Id}, zero())
			if err != nil {
				return noOpErr(label, err)
			}
			if !hid {
				return noOp(label, "no hidden reply and none could be hidden")
			}
		}
		acc, _ := e.postOwner(rp)
		return e.submit(acc, &types.MsgUnhideReply{Creator: acc.Address.String(), Id: rp.Id}, zero())
	})
}
