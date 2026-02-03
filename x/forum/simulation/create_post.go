package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgCreatePost simulates a MsgCreatePost message using direct keeper calls.
// This bypasses spam tax and rate limiting requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgCreatePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a category
		categoryID, err := getOrCreateCategory(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreatePost{}), "failed to get/create category"), nil, nil
		}

		// Decide whether to create a root post or a reply
		var parentID uint64 = 0
		var rootID uint64 = 0
		var depth uint64 = 0

		if r.Intn(3) > 0 { // 66% chance of being a reply
			// Try to find an existing root post to reply to
			rootPost, rootPostID, findErr := findUnlockedRootPost(r, ctx, k)
			if findErr == nil && rootPost != nil {
				parentID = rootPostID
				rootID = rootPostID
				categoryID = rootPost.CategoryId
				depth = 1
			}
		}

		// Use direct keeper calls to create post (bypasses spam tax, rate limiting, membership checks)
		postID, err := k.PostSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreatePost{}), "failed to get post ID"), nil, nil
		}

		// For root posts, rootID is the post itself
		if parentID == 0 {
			rootID = postID
		}

		now := ctx.BlockTime().Unix()
		post := types.Post{
			PostId:     postID,
			CategoryId: categoryID,
			RootId:     rootID,
			ParentId:   parentID,
			Author:     simAccount.Address.String(),
			Content:    randomContent(r),
			CreatedAt:  now,
			Status:     types.PostStatus_POST_STATUS_ACTIVE,
			Depth:      depth,
		}

		if err := k.Post.Set(ctx, postID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreatePost{}), "failed to create post"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreatePost{}), "ok (direct keeper call)"), nil, nil
	}
}
