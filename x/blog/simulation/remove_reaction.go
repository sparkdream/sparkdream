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

// SimulateMsgRemoveReaction simulates a MsgRemoveReaction message using direct keeper calls.
func SimulateMsgRemoveReaction(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgRemoveReaction{})
		creator := simAccount.Address.String()

		// Find or create a post, then ensure a reaction exists
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		postID, err := getOrCreateAnyActivePost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create active post"), nil, nil
		}

		if err := getOrCreateReaction(r, ctx, k, postID, 0, creator); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create reaction"), nil, nil
		}

		// Get the reaction to know its type for count adjustment
		reaction, found := k.GetReaction(ctx, postID, 0, creator)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "reaction not found after creation"), nil, nil
		}

		// Remove the reaction
		k.RemoveReaction(ctx, postID, 0, creator)

		// Decrement reaction counts
		counts := k.GetReactionCounts(ctx, postID, 0)
		decrementReactionCount(&counts, reaction.ReactionType)
		k.SetReactionCounts(ctx, postID, 0, counts)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
