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

func SimulateMsgRespondToChallenge(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// NOTE: This operation cannot succeed in simulation because responding to a challenge
		// triggers jury selection, which requires 7 eligible jurors to be set up with sufficient
		// staked DREAM. This complex setup cannot be reasonably done within simulation.
		// In a real chain environment, jurors would be available from staked members.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRespondToChallenge{}), "skipped: requires eligible jurors"), nil, nil
	}
}
