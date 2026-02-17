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
)

// SimulateMsgSpendFromCommons simulates a MsgSpendFromCommons message using direct keeper calls.
// This bypasses the group policy authorization requirement for simulation purposes.
// Full authorization testing should be done in integration tests.
func SimulateMsgSpendFromCommons(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// Get the commons module account balance
		moduleAddr := ak.GetModuleAddress(types.ModuleName)
		spendableCoins := bk.SpendableCoins(ctx, moduleAddr)
		if spendableCoins.Empty() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpendFromCommons{}), "no coins in the commons pool"), nil, nil
		}

		// Select a random recipient and amount
		recipient, _ := simtypes.RandomAcc(r, accs)

		denomIndex := r.Intn(len(spendableCoins))
		coin := spendableCoins[denomIndex]

		// Spend between 1 and 10% of the available amount
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

		// Use direct bank keeper call to send coins (bypasses group policy authorization)
		if err := bk.SendCoins(ctx, moduleAddr, recipient.Address, spendAmount); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpendFromCommons{}), "send failed"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpendFromCommons{}), "ok (direct keeper call)"), nil, nil
	}
}
