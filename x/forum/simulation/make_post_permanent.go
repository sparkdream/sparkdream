package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgMakePostPermanent simulates a MsgMakePostPermanent message using
// direct keeper calls. Mirrors the blog simulation's promote-only behavior:
// finds (or creates) an ephemeral post, clears its ExpirationTime and the
// ExpirationQueue + EphemeralByAuthor entries, without touching pin markers.
func SimulateMsgMakePostPermanent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgMakePostPermanent{})

		// Find an existing ephemeral post or create one for the sim account.
		postID, err := getOrCreateEphemeralPost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get/create ephemeral post"), nil, nil
		}

		post, err := k.Post.Get(ctx, postID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to load post"), nil, nil
		}

		// Idempotent: already permanent is a no-op (and a legitimate success).
		if post.ExpirationTime == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (already permanent)"), nil, nil
		}

		oldExpiresAt := post.ExpirationTime
		post.ExpirationTime = 0
		if post.ConvictionSustained {
			post.ConvictionSustained = false
		}
		if err := k.Post.Set(ctx, postID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to update post"), nil, nil
		}
		_ = k.ExpirationQueue.Remove(ctx, collections.Join(oldExpiresAt, postID))
		k.RemoveEphemeralAuthorIndex(ctx, post.Author, postID)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
