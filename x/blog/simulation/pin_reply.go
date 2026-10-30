package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgPinReply delivers MsgPinReply on a random active, permanent,
// unpinned reply from a live member at pin_min_trust_level. With no such
// reply, a member (whose replies are permanent) replies first.
func SimulateMsgPinReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgPinReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgPinReply{}
		pinner, ok := e.pinner()
		if !ok {
			return noOp(label, "no live member at pin_min_trust_level")
		}
		pinnable := func(rp types.Reply) bool { return activeReply(rp) && rp.ExpiresAt == 0 && rp.PinnedBy == "" }
		rp, ok, err := e.replyOrCreate(pinnable, e.member)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no permanent unpinned reply and none could be created")
		}
		return e.submit(pinner, &types.MsgPinReply{Creator: pinner.Address.String(), Id: rp.Id}, zero())
	})
}
