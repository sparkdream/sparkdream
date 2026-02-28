package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func SimulateMsgNominate(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txCfg client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgNominate{}
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "Nominate simulation not implemented"), nil, nil
	}
}
