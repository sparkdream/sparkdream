package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

func SimulateMsgSubmitCheckpoint(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgSubmitCheckpoint{
			Operator: simAccount.Address.String(),
		}

		// Skipped: a checkpoint needs a registered ACTIVE operator, and the
		// service simulation registers none (operators require a commons
		// Group controller). Keeper tests cover the handler and liveness.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no registered operators in simulation"), nil, nil
	}
}
