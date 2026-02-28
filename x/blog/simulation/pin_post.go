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

// SimulateMsgPinPost simulates a MsgPinPost message using direct keeper calls.
func SimulateMsgPinPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgPinPost{})

		// Find or create an ephemeral post to pin
		postID, err := getOrCreateEphemeralPost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create ephemeral post"), nil, nil
		}

		post, found := k.GetPost(ctx, postID)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "post not found"), nil, nil
		}

		// Remove from expiry index
		if post.ExpiresAt > 0 {
			k.RemoveFromExpiryIndex(ctx, post.ExpiresAt, "post", post.Id)
		}

		// Pin the post
		post.ExpiresAt = 0
		post.PinnedBy = simAccount.Address.String()
		post.PinnedAt = ctx.BlockTime().Unix()
		k.SetPost(ctx, post)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
