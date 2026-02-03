package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
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

		// 1. Check for spendable uspark coins (market creation uses uspark specifically)
		spendable := bk.SpendableCoins(ctx, simAccount.Address)
		usparkBalance := spendable.AmountOf("uspark")
		if usparkBalance.IsZero() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateMarket{}), "No uspark balance"), nil, nil
		}

		// 2. Ensure enough balance for liquidity + fees
		// Minimum liquidity required is 100000, need at least 3x for fees buffer
		minLiquidity := math.NewInt(100000)
		minBalance := minLiquidity.MulRaw(3) // Need 3x min liquidity to have buffer for fees
		if usparkBalance.LT(minBalance) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateMarket{}), "Balance too low for market creation"), nil, nil
		}
		// Use 10% of available balance for liquidity, leaving 90% for fees
		// This is more conservative to avoid fee calculation issues
		amount := usparkBalance.QuoRaw(10)
		if amount.LT(minLiquidity) {
			amount = minLiquidity
		}
		liquidity := sdk.NewCoin("uspark", amount)

		msg := &types.MsgCreateMarket{
			Creator:          simAccount.Address.String(),
			Symbol:           simtypes.RandStringOfLength(r, 5),
			Question:         simtypes.RandStringOfLength(r, 20),
			InitialLiquidity: &liquidity.Amount,
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
