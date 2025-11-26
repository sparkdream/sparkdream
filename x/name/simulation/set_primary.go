package simulation

import (
	"math/rand"
	"strings"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgSetPrimary(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Select a random simulation account
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. Generate random data matching RegisterName constraints
		// Logic from RegisterName Step 1: Normalize Name
		name := strings.ToLower(simtypes.RandStringOfLength(r, 10))
		data := simtypes.RandStringOfLength(r, 25)

		// 3. Setup Pre-conditions: Inject State
		// We manually register the name to avoid paying fees or passing Council checks in the sim.
		record := types.NameRecord{
			Name:  name,
			Owner: simAccount.Address.String(),
			Data:  data,
		}

		// A. Save the main record
		err := k.SetName(ctx, record)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetPrimary{}), "failed to set name record"), nil, err
		}

		// B. Update the secondary index (Owner -> Names)
		// This is required because your RegisterName function calls AddNameToOwner.
		// Without this, the account technically owns the name, but the lookup index would be empty.
		err = k.AddNameToOwner(ctx, simAccount.Address, name)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetPrimary{}), "failed to update owner index"), nil, err
		}

		// 4. Construct the MsgSetPrimary
		msg := &types.MsgSetPrimary{
			Authority: simAccount.Address.String(),
			Name:      name,
		}

		// 5. Execute Transaction
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: nil,
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
