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

// SimulateMsgHidePost simulates a MsgHidePost message using direct keeper calls.
func SimulateMsgHidePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgHidePost{})

		// Find or create an active post to hide
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		postID, err := getOrCreateActivePost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create active post"), nil, nil
		}

		post, found := k.GetPost(ctx, postID)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "post not found"), nil, nil
		}

		if post.Status != types.PostStatus_POST_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "post not active"), nil, nil
		}

		// Hide the post via direct state manipulation
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		post.HiddenBy = simAccount.Address.String()
		post.HiddenAt = ctx.BlockTime().Unix()
		k.SetPost(ctx, post)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
