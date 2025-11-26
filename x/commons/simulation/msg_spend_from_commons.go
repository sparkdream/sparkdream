package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	"github.com/cosmos/cosmos-sdk/x/simulation"
)

func SimulateMsgSpendFromCommons(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Get the authorized Council Address from params
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpendFromCommons{}), "failed to get params"), nil, nil
		}
		councilAddrStr := params.CommonsCouncilAddress

		// 2. Find if we control this address
		var simAccount simtypes.Account
		var found bool

		councilAddr, err := sdk.AccAddressFromBech32(councilAddrStr)
		if err == nil {
			simAccount, found = simtypes.FindAccount(accs, councilAddr)
		}

		if !found {
			// If we can't sign as the authorized council, we pick a random account.
			// This effectively tests the "Unauthorized" error path.
			simAccount, _ = simtypes.RandomAcc(r, accs)
		}

		// 3. Get the commons module account balance
		moduleAddr := ak.GetModuleAddress(types.ModuleName)
		spendableCoins := bk.SpendableCoins(ctx, moduleAddr)
		if spendableCoins.Empty() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpendFromCommons{}), "no coins in the commons pool"), nil, nil
		}

		// 4. Select a random recipient and amount
		recipient, _ := simtypes.RandomAcc(r, accs)

		// Select a random coin and amount from the spendable coins
		denomIndex := r.Intn(len(spendableCoins))
		coin := spendableCoins[denomIndex]

		// Spend between 1 and 10% of the available amount of the chosen coin
		maxAmt := coin.Amount.Quo(math.NewInt(10))
		if maxAmt.IsZero() {
			maxAmt = math.NewInt(1)
		}
		if coin.Amount.IsZero() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpendFromCommons{}), "selected coin has zero amount"), nil, nil
		}

		amount, err := simtypes.RandPositiveInt(r, maxAmt)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpendFromCommons{}), "failed to generate random amount"), nil, nil
		}

		spendAmount := sdk.NewCoins(sdk.NewCoin(coin.Denom, amount))

		// 5. Construct the message
		msg := &types.MsgSpendFromCommons{
			Authority: simAccount.Address.String(),
			Recipient: recipient.Address.String(),
			Amount:    spendAmount,
		}

		// 6. Construct and execute the operation
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: spendAmount,
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
