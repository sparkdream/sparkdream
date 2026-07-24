package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func SimulateMsgUpdatePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Get the count of posts
		count := k.GetPostCount(ctx)
		if count == 0 {
			// No posts to update
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePost{}), "no posts to update"), nil, nil
		}

		// 2. Pick a random post ID
		var post types.Post
		var found bool
		// Try to find a valid active post. Posts may have been deleted or hidden.
		for i := 0; i < 100; i++ {
			postID := r.Uint64() % count
			post, found = k.GetPost(ctx, postID)
			if found && post.Status == types.PostStatus_POST_STATUS_ACTIVE {
				break
			}
			found = false
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePost{}), "no active post found"), nil, nil
		}

		// 3. Find the simulation account that owns this post
		creatorAddr, err := sdk.AccAddressFromBech32(post.Creator)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePost{}), "invalid creator address"), nil, err
		}

		simAccount, found := simtypes.FindAccount(accs, creatorAddr)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePost{}), "creator not found in simulation accounts"), nil, nil
		}

		// 4. Create the Update message with new random content
		newTitle := simtypes.RandStringOfLength(r, 25)
		newBody := simtypes.RandStringOfLength(r, 250)
		msg := &types.MsgUpdatePost{
			Creator: simAccount.Address.String(),
			Id:      post.Id,
			Title:   newTitle,
			Body:    newBody,
		}

		// 4b. Ensure the creator can cover the high-water storage delta fee the
		// handler will charge (CostPerByteAmount * bytes above the post's
		// previous high-water mark). Without this precheck a larger edit can
		// deliver a tx the creator cannot pay, failing the whole simulation.
		params, perr := k.Params.Get(ctx)
		if perr == nil && !params.CostPerByteExempt && params.CostPerByteAmount.IsPositive() {
			newBytes := uint64(len(newTitle) + len(newBody))
			if newBytes > post.FeeBytesHighWater {
				delta := int64(newBytes - post.FeeBytesHighWater)
				deltaFee := params.CostPerByteAmount.MulRaw(delta)
				bondDenom := k.BondDenom(ctx)
				needed := deltaFee.Add(math.NewInt(10000)) // + gas headroom
				if bk.SpendableCoins(ctx, simAccount.Address).AmountOf(bondDenom).LT(needed) {
					return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "creator cannot cover storage delta fee"), nil, nil
				}
			}
		}

		// 5. Construct the OperationInput struct
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: nil,
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		// 6. Execute
		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
