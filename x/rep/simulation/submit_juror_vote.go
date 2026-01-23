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

func SimulateMsgSubmitJurorVote(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// NOTE: This operation cannot succeed in simulation because juror voting requires:
		// 1. An active JuryReview (created when a challenge is responded to)
		// 2. The juror must be selected for that specific jury
		// 3. The jury must have enough eligible members (7+ with sufficient staked DREAM)
		// Setting up this state correctly is complex and cannot be done reliably in simulation.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitJurorVote{}), "skipped: requires active JuryReview with selected jurors"), nil, nil
	}
}
