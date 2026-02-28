package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgUpdateReply simulates a MsgUpdateReply message using direct keeper calls.
func SimulateMsgUpdateReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgUpdateReply{})

		// Find or create a post, then a reply to update
		postID, err := getOrCreateAnyActivePost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create active post"), nil, nil
		}

		replyID, err := getOrCreateReplyOnPost(r, ctx, k, postID, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create reply"), nil, nil
		}

		reply, found := k.GetReply(ctx, replyID)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "reply not found"), nil, nil
		}

		// Update the reply body
		reply.Body = randomBody(r)
		reply.Edited = true
		reply.EditedAt = ctx.BlockTime().Unix()
		k.SetReply(ctx, reply)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
