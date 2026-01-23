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

func SimulateMsgCompleteInitiative(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// NOTE: This operation cannot succeed in simulation because completing
		// an initiative requires meeting complex requirements:
		// 1. Sufficient conviction score from stakers
		// 2. Approved work submission
		// 3. Proper initiative status transitions
		// Setting up this state correctly is complex and cannot be done reliably in simulation.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCompleteInitiative{}), "skipped: requires completion requirements to be met"), nil, nil
	}
}
