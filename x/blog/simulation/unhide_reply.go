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

// SimulateMsgUnhideReply simulates a MsgUnhideReply message using direct keeper calls.
func SimulateMsgUnhideReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgUnhideReply{})

		// Need a post first, then a hidden reply on it
		postID, err := getOrCreateAnyActivePost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create active post"), nil, nil
		}

		replyID, err := getOrCreateHiddenReply(r, ctx, k, postID, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create hidden reply"), nil, nil
		}

		reply, found := k.GetReply(ctx, replyID)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "reply not found"), nil, nil
		}

		// Unhide the reply
		reply.Status = types.ReplyStatus_REPLY_STATUS_ACTIVE
		reply.HiddenBy = ""
		reply.HiddenAt = 0
		k.SetReply(ctx, reply)

		// Increment post reply count
		post, found := k.GetPost(ctx, postID)
		if found {
			post.ReplyCount++
			k.SetPost(ctx, post)
		}

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
