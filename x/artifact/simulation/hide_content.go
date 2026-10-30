package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

func SimulateMsgHideContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgHideContent{
			Authority: simAccount.Address.String(),
		}

		// Not simulated: MsgHideContent needs a bonded content sentinel or a council policy, which the simulation does not model.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "HideContent needs a bonded content sentinel or a council policy, which the simulation does not model"), nil, nil
	}
}
