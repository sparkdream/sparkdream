package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgFlagPost simulates a MsgFlagPost message using direct keeper calls.
// This bypasses spam tax requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgFlagPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find or create a post to flag
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		postID, err := getOrCreatePost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFlagPost{}), "failed to create post"), nil, nil
		}

		// Use direct keeper calls to flag post (bypasses spam tax)
		postFlag, err := k.PostFlag.Get(ctx, postID)
		if err != nil {
			// Create new flag record
			postFlag = types.PostFlag{
				PostId:        postID,
				TotalWeight:   "0",
				InReviewQueue: false,
				Flaggers:      []string{},
			}
		}

		// Check if already flagged by this user
		for _, flagger := range postFlag.Flaggers {
			if flagger == simAccount.Address.String() {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFlagPost{}), "already flagged this post"), nil, nil
			}
		}

		// Add flagger
		postFlag.Flaggers = append(postFlag.Flaggers, simAccount.Address.String())
		postFlag.TotalWeight = fmt.Sprintf("%d", len(postFlag.Flaggers))

		if err := k.PostFlag.Set(ctx, postID, postFlag); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFlagPost{}), "failed to flag post"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFlagPost{}), "ok (direct keeper call)"), nil, nil
	}
}
