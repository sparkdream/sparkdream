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

// SimulateMsgUnhidePost simulates a MsgUnhidePost message using direct keeper calls.
func SimulateMsgUnhidePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgUnhidePost{})

		// Find or create a hidden post to unhide
		postID, err := getOrCreateHiddenPost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create hidden post"), nil, nil
		}

		post, found := k.GetPost(ctx, postID)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "post not found"), nil, nil
		}

		// Unhide the post
		post.Status = types.PostStatus_POST_STATUS_ACTIVE
		post.HiddenBy = ""
		post.HiddenAt = 0
		k.SetPost(ctx, post)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
