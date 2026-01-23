package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgApproveProjectBudget(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// NOTE: This operation cannot succeed in simulation because the approver
		// must be a member of the Operations Committee, which requires external
		// x/group governance setup that cannot be done within simulation.
		// Rather than failing the simulation, we return a NoOp and skip this message.
		// In a real chain environment, committee membership would be established via x/group.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgApproveProjectBudget{}), "skipped: requires committee membership"), nil, nil
	}
}
