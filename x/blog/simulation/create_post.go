package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func SimulateMsgCreatePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Pick a random account to create the post
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. Generate random content for the message
		msg := &types.MsgCreatePost{
			Creator: simAccount.Address.String(),
			Title:   simtypes.RandStringOfLength(r, 20),  // Random title (20 chars)
			Body:    simtypes.RandStringOfLength(r, 200), // Random body (200 chars)
		}

		// 3. Construct the OperationInput struct expected by util.go
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil, // Codec is not strictly used in the helper logic for GenTx
			Msg:             msg,
			CoinsSpentInMsg: nil, // No coins spent by this message
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		// 4. Call the helper function (defined in util.go in the same package)
		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
