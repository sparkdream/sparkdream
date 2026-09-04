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

func SimulateMsgAssignInterim(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// NOTE: This operation cannot succeed in simulation because assigning
		// interim work requires Operations Committee authority — being an
		// assignee entitles you to a share of the interim's budget, so adding
		// one is a spending decision. That authority comes from the x/commons
		// council/committee bootstrap, which simulation does not run, so no
		// random account can hold it. Rather than failing the simulation, we
		// return a NoOp and skip this message, exactly as
		// SimulateMsgApproveInterim and SimulateMsgApproveProjectBudget do.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignInterim{}), "skipped: requires committee membership"), nil, nil
	}
}
