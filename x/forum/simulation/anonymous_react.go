package simulation

import (
	"encoding/hex"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgAnonymousReact simulates a MsgAnonymousReact message using direct keeper calls.
// Anonymous reactions require ZK proofs which cannot be generated in simulation, so we use direct state writes.
func SimulateMsgAnonymousReact(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgAnonymousReact{})

		// Find an active post to react to
		post, postID, err := findPostWithStatus(r, ctx, k, types.PostStatus_POST_STATUS_ACTIVE)
		if err != nil || post == nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active post found"), nil, nil
		}

		// Randomly upvote or downvote
		if r.Intn(2) == 0 {
			post.UpvoteCount++
		} else {
			post.DownvoteCount++
		}
		if err := k.Post.Set(ctx, postID, *post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to update post"), nil, nil
		}

		// Record nullifier (domain=5 for forum reactions)
		fakeNullifier := make([]byte, 32)
		r.Read(fakeNullifier)
		nullifierHex := hex.EncodeToString(fakeNullifier)
		entry := types.AnonNullifierEntry{
			UsedAt: ctx.BlockTime().Unix(),
			Domain: 5,
			Scope:  postID,
		}
		k.SetNullifierUsed(ctx, 5, postID, nullifierHex, entry)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
