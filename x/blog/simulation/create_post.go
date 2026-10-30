package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgCreatePost delivers MsgCreatePost from a random live account.
// Members post permanently, non-members ephemerally; the signer pays the
// storage fee (and the media scan fee on a media post), and may attach a
// DREAM author bond (required for media below media_min_trust_level).
func SimulateMsgCreatePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgCreatePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		acc, ok := pick(e.r, e.live(anyone))
		if !ok {
			return noOp(&types.MsgCreatePost{}, "no live account")
		}
		msg, spent := e.newPostMsg(acc)
		return e.submit(acc, msg, spent)
	})
}
