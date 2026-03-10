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
		title := simtypes.RandStringOfLength(r, 20)
		body := simtypes.RandStringOfLength(r, 200)

		// Estimate storage fee: CostPerByte * (title + body length)
		// Default CostPerByte is 100uspark, 220 chars -> ~22000uspark
		contentLen := int64(len(title) + len(body))
		storageFee := sdk.NewCoin("uspark", math.NewInt(100).MulRaw(contentLen))

		// Check solvency: need storage fee + some gas
		balance := bk.SpendableCoins(ctx, simAccount.Address)
		totalRequired := storageFee.Amount.Add(math.NewInt(10000))
		if balance.AmountOf("uspark").LT(totalRequired) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreatePost{}), "insufficient funds for storage fee"), nil, nil
		}

		msg := &types.MsgCreatePost{
			Creator: simAccount.Address.String(),
			Title:   title,
			Body:    body,
		}

		// 3. Declare storage fee as CoinsSpentInMsg so GenAndDeliverTxWithRandFees
		// reserves this amount and only uses the remainder for random fees
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(storageFee),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
