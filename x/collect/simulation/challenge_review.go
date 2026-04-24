package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

func SimulateMsgChallengeReview(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgChallengeReview{
			Creator: simAccount.Address.String(),
		}

		// Try to find an unchallenged review
		review, reviewID, err := findUnchallengedReview(r, ctx, k)
		if err != nil || review == nil {
			// None found: create a curator, collection, and review to challenge
			curatorAccount, _ := simtypes.RandomAcc(r, accs)
			if err := getOrCreateCurator(r, ctx, k, curatorAccount.Address.String()); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create curator: "+err.Error()), nil, nil
			}

			ownerAccount, ok := pickDifferentAccount(r, accs, curatorAccount.Address.String())
			if !ok {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "not enough accounts"), nil, nil
			}

			collID, err := getOrCreateCollection(r, ctx, k, ownerAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create collection: "+err.Error()), nil, nil
			}

			reviewID, err = getOrCreateCurationReview(r, ctx, k, curatorAccount.Address.String(), collID)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create review: "+err.Error()), nil, nil
			}

			rev, err := k.CurationReview.Get(ctx, reviewID)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get review: "+err.Error()), nil, nil
			}
			review = &rev
		}

		// Mark review as challenged
		review.Challenged = true
		review.Challenger = simAccount.Address.String()
		if err := k.CurationReview.Set(ctx, reviewID, *review); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update review: "+err.Error()), nil, nil
		}

		// Increment per-module challenged-review counter. Slash-bond
		// reservation against the rep BondedRole is skipped here (simulation
		// cannot seed cross-module state); the corresponding actual msg
		// handler does reserve bond at runtime.
		activity, _ := k.CuratorActivity.Get(ctx, review.Curator)
		if activity.Address == "" {
			activity.Address = review.Curator
		}
		activity.ChallengedReviews++
		if err := k.CuratorActivity.Set(ctx, review.Curator, activity); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update curator activity: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
