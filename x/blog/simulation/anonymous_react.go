package simulation

import (
	"encoding/hex"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgAnonymousReact simulates a MsgAnonymousReact message using direct keeper calls.
// Anonymous reactions require ZK proofs which cannot be generated in simulation.
func SimulateMsgAnonymousReact(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgAnonymousReact{})

		// Pick a random existing post
		postCount := k.GetPostCount(ctx)
		if postCount == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no posts"), nil, nil
		}

		postID := uint64(r.Intn(int(postCount))) + 1
		post, found := k.GetPost(ctx, postID)
		if !found || post.Status != types.PostStatus_POST_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "post not active"), nil, nil
		}

		// Pick a random reaction type (1-4)
		reactionType := types.ReactionType(r.Intn(4) + 1)

		// Increment the reaction count directly
		counts := k.GetReactionCounts(ctx, postID, 0)
		switch reactionType {
		case types.ReactionType_REACTION_TYPE_LIKE:
			counts.LikeCount++
		case types.ReactionType_REACTION_TYPE_INSIGHTFUL:
			counts.InsightfulCount++
		case types.ReactionType_REACTION_TYPE_DISAGREE:
			counts.DisagreeCount++
		case types.ReactionType_REACTION_TYPE_FUNNY:
			counts.FunnyCount++
		}
		k.SetReactionCounts(ctx, postID, 0, counts)

		// Record nullifier (domain=8 for post reactions)
		fakeNullifier := make([]byte, 32)
		r.Read(fakeNullifier)
		nullifierHex := hex.EncodeToString(fakeNullifier)
		k.SetNullifierUsed(ctx, 8, postID, nullifierHex, types.AnonNullifierEntry{
			UsedAt: ctx.BlockTime().Unix(),
			Domain: 8,
			Scope:  postID,
		})

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
