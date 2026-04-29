package simulation

import (
	"math/rand"
	"strings"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgUpdateName(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Pick a random account to be the owner
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// We define the high fee required by our application logic
		requiredFee := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(5000000)))

		// Check if the simulation account actually has these funds
		spendable := bk.SpendableCoins(ctx, simAccount.Address)
		if !spendable.IsAllGTE(requiredFee) {
			// If insufficient funds, we SKIP this iteration (NoOp) gracefully.
			// This prevents the "insufficient funds" crash in the test runner.
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateName{}), "skipped: insufficient funds for fee"), nil, nil
		}

		// 2. Generate a random name and data
		// We use a safe length (e.g. 10 chars) to satisfy min/max params.
		// Lowercase to match MsgUpdateName's normalization (strings.ToLower)
		// — mixed-case keys would not be found on lookup.
		name := strings.ToLower(simtypes.RandStringOfLength(r, 10))
		initialData := "initial_sim_data"

		// 3. SEED STATE: Create the name directly in the store
		// We bypass MsgRegisterName handlers here to ensure the state exists
		// so we can specifically test MsgUpdateName.
		record := types.NameRecord{
			Name:  name,
			Owner: simAccount.Address.String(),
			Data:  initialData,
		}

		// Store the name record
		if err := k.SetName(ctx, record); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateName{}), "failed to seed name"), nil, err
		}

		// Update indices (important so the system recognizes ownership)
		if err := k.AddNameToOwner(ctx, simAccount.Address, name); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateName{}), "failed to seed owner index"), nil, err
		}

		// 4. Construct the Update Message
		newMetadata := simtypes.RandStringOfLength(r, 25)

		msg := &types.MsgUpdateName{
			Creator: simAccount.Address.String(),
			Name:    name,
			Data:    newMetadata,
		}

		// 5. Execute Transaction
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		// Define explicit high fees to satisfy the AnteHandler check (5M uspark)
		// Random fees are usually too low for the x/commons spam protection.
		fees := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(5000000)))

		// Use GenAndDeliverTx (explicit fees) instead of GenAndDeliverTxWithRandFees
		return simulation.GenAndDeliverTx(opMsg, fees)
	}
}
