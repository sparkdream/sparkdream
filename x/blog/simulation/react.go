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

// SimulateMsgReact simulates a MsgReact message using direct keeper calls.
func SimulateMsgReact(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgReact{})

		// Find or create a post to react to
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		postID, err := getOrCreateAnyActivePost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create active post"), nil, nil
		}

		// React to the post (replyId = 0 means post-level reaction)
		rt := randomReactionType(r)
		creator := simAccount.Address.String()

		// Check if already reacted
		_, found := k.GetReaction(ctx, postID, 0, creator)
		if found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "already reacted"), nil, nil
		}

		reaction := types.Reaction{
			Creator:      creator,
			ReactionType: rt,
			PostId:       postID,
			ReplyId:      0,
		}
		k.SetReaction(ctx, reaction)

		// Increment reaction counts
		counts := k.GetReactionCounts(ctx, postID, 0)
		incrementReactionCount(&counts, rt)
		k.SetReactionCounts(ctx, postID, 0, counts)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
