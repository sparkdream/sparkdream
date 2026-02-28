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

// SimulateMsgCreateReply simulates a MsgCreateReply message using direct keeper calls.
func SimulateMsgCreateReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgCreateReply{})

		// Find or create a post to reply to
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		postID, err := getOrCreateAnyActivePost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create active post"), nil, nil
		}

		// Create the reply
		reply := types.Reply{
			PostId:    postID,
			Creator:   simAccount.Address.String(),
			Body:      randomBody(r),
			CreatedAt: ctx.BlockTime().Unix(),
			Status:    types.ReplyStatus_REPLY_STATUS_ACTIVE,
			Depth:     1,
		}
		k.AppendReply(ctx, reply)

		// Increment post reply count
		post, found := k.GetPost(ctx, postID)
		if found {
			post.ReplyCount++
			k.SetPost(ctx, post)
		}

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
