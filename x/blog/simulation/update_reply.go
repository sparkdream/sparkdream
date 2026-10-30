package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
)

// SimulateMsgUpdateReply delivers MsgUpdateReply from the author (by alias)
// of a random active reply, creating a reply first when none has a live sim
// author. The signer pays the storage fee for bytes above the reply's fee
// high-water mark, plus the media scan fee when the new body is media.
func SimulateMsgUpdateReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpdateReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgUpdateReply{}
		owned := func(rp types.Reply) bool {
			_, ok := e.owner(rp.Creator)
			return activeReply(rp) && ok
		}
		rp, ok, err := e.replyOrCreate(owned, anyone)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no active reply with a live author and none could be created")
		}
		acc, _ := e.owner(rp.Creator)
		ct, body, _ := e.randomContent(acc.Address.String(), e.params.MaxReplyLength, false)
		msg := &types.MsgUpdateReply{
			Creator:     acc.Address.String(),
			Id:          rp.Id,
			Body:        body,
			ContentType: ct,
		}
		spent := e.scanFee(ct, body)
		newBytes := commontypes.Saturate[uint64](len(msg.Body))
		if newBytes > rp.FeeBytesHighWater {
			spent = spent.Add(e.storageFee(commontypes.Saturate[int](newBytes - rp.FeeBytesHighWater)))
		}
		return e.submit(acc, msg, spent)
	})
}
