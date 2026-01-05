package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
)

func SimulateMsgCreateMarket(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 1. Check for spendable coins to determine InitialLiquidity
		spendable := bk.SpendableCoins(ctx, simAccount.Address)
		if spendable.Empty() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateMarket{}), "No spendable coins"), nil, nil
		}

		// 2. Select a random coin for liquidity, ensuring we leave some for fees
		// We'll take a small fraction to be safe
		coin := spendable[r.Intn(len(spendable))]
		amount := coin.Amount.QuoRaw(10) // Use 10% of available balance
		if amount.IsZero() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateMarket{}), "Balance too low for liquidity"), nil, nil
		}
		liquidity := sdk.NewCoin(coin.Denom, amount)

		msg := &types.MsgCreateMarket{
			Creator:          simAccount.Address.String(),
			Symbol:           simtypes.RandStringOfLength(r, 5),
			Question:         simtypes.RandStringOfLength(r, 20),
			InitialLiquidity: liquidity.String(),
			EndBlock:         ctx.BlockHeight() + int64(r.Intn(100)+10), // At least 10 blocks in future
		}

		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(liquidity),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
