package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

func SimulateMsgTriggerDkg(
	ak types.AccountKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// MsgTriggerDkg is governance-gated (requires authority).
		// Simulation accounts cannot act as governance, so return NoOp.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTriggerDkg{}), "governance-gated message"), nil, nil
	}
}
