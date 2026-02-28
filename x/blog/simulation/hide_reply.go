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

// SimulateMsgHideReply simulates a MsgHideReply message using direct keeper calls.
func SimulateMsgHideReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgHideReply{})

		// Find or create a post, then a reply on it
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		postID, err := getOrCreateAnyActivePost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create active post"), nil, nil
		}

		replyID, err := getOrCreateReplyOnPost(r, ctx, k, postID, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create reply"), nil, nil
		}

		reply, found := k.GetReply(ctx, replyID)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "reply not found"), nil, nil
		}

		if reply.Status != types.ReplyStatus_REPLY_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "reply not active"), nil, nil
		}

		// Hide the reply
		reply.Status = types.ReplyStatus_REPLY_STATUS_HIDDEN
		reply.HiddenBy = simAccount.Address.String()
		reply.HiddenAt = ctx.BlockTime().Unix()
		k.SetReply(ctx, reply)

		// Decrement post reply count
		post, found := k.GetPost(ctx, postID)
		if found && post.ReplyCount > 0 {
			post.ReplyCount--
			k.SetPost(ctx, post)
		}

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
