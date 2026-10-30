package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgMakeReplyPermanent delivers MsgMakeReplyPermanent on a random
// active, unexpired ephemeral reply from a live member at
// make_permanent_min_trust_level. With no ephemeral reply, a non-member
// replies first (on a post open to all, or on their own post).
func SimulateMsgMakeReplyPermanent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgMakeReplyPermanent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgMakeReplyPermanent{}
		promoter, ok := e.promoter()
		if !ok {
			return noOp(label, "no live member at make_permanent_min_trust_level")
		}
		ephemeral := func(rp types.Reply) bool { return activeReply(rp) && rp.ExpiresAt > e.now() }
		rp, ok, err := e.replyOrCreate(ephemeral, func(a simtypes.Account) bool { return !e.member(a) })
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no ephemeral reply and none could be created")
		}
		return e.submit(promoter, &types.MsgMakeReplyPermanent{Creator: promoter.Address.String(), Id: rp.Id}, zero())
	})
}
