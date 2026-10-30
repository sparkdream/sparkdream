package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgUnpinReply delivers MsgUnpinReply on a random active pinned
// reply from a live member at pin_min_trust_level. With no pinned reply, a
// member pins one first (a real MsgPinReply).
func SimulateMsgUnpinReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnpinReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgUnpinReply{}
		pinner, ok := e.pinner()
		if !ok {
			return noOp(label, "no live member at pin_min_trust_level")
		}
		rp, ok := pick(e.r, e.repliesWhere(func(rp types.Reply) bool { return activeReply(rp) && rp.PinnedBy != "" }))
		if !ok {
			rp, ok = pick(e.r, e.repliesWhere(func(rp types.Reply) bool { return activeReply(rp) && rp.ExpiresAt == 0 && rp.PinnedBy == "" }))
			if !ok {
				return noOp(label, "no pinned reply and no permanent reply to pin")
			}
			pinned, err := e.ensure(pinner, &types.MsgPinReply{Creator: pinner.Address.String(), Id: rp.Id}, zero())
			if err != nil {
				return noOpErr(label, err)
			}
			if !pinned {
				return noOp(label, "no pinned reply and none could be pinned")
			}
		}
		return e.submit(pinner, &types.MsgUnpinReply{Creator: pinner.Address.String(), Id: rp.Id}, zero())
	})
}
