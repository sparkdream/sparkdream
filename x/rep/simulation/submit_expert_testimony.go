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

func SimulateMsgSubmitExpertTestimony(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// NOTE: This operation cannot succeed in simulation because expert testimony requires:
		// 1. An active JuryReview (created when a challenge is responded to)
		// 2. The expert must be qualified for the specific tags
		// 3. The testimony must be submitted within the proper time window
		// Setting up this state correctly is complex and cannot be done reliably in simulation.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitExpertTestimony{}), "skipped: requires active JuryReview"), nil, nil
	}
}
