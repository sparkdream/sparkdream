package simulation

import (
	"math/rand"
	"slices"

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

		// 1. Determine the Target Permission (RBAC)
		// We need to find an account that is allowed to send this specific message.
		targetMsgType := sdk.MsgTypeURL(&types.MsgSpendFromCommons{})

		var simAccount simtypes.Account
		var found bool

		// 2. Search for an Authorized Account in the Sim List
		// We iterate through available keys to see if any have been granted the specific permission.
		// (This supports the new PolicyPermissions architecture).
		for _, acc := range accs {
			perms, err := k.PolicyPermissions.Get(ctx, acc.Address.String())
			if err == nil {
				// Check if the permission list contains the target message
				if slices.Contains(perms.AllowedMessages, targetMsgType) {
					simAccount = acc
					found = true
					break
				}
			}
		}

		// 3. Fallback / Chaos Testing
		// If we didn't find an authorized account, OR if we randomly decide to test the failure path (50% chance),
		// we pick a random account. This ensures we test "Unauthorized" errors.
		if !found || r.Intn(2) == 0 {
			simAccount, _ = simtypes.RandomAcc(r, accs)
		}

		// 4. Get the commons module account balance
		moduleAddr := ak.GetModuleAddress(types.ModuleName)
		spendableCoins := bk.SpendableCoins(ctx, moduleAddr)
		if spendableCoins.Empty() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpendFromCommons{}), "no coins in the commons pool"), nil, nil
		}

		// 5. Select a random recipient and amount
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

		// 6. Construct the message
		msg := &types.MsgSpendFromCommons{
			Authority: simAccount.Address.String(),
			Recipient: recipient.Address.String(),
			Amount:    spendAmount,
		}

		// 7. Construct and execute the operation
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
