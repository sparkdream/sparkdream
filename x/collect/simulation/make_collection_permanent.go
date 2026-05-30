package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgMakeCollectionPermanent is a no-op for the same reason as
// SimulateMsgPinCollection: the make-permanent path requires a trust-level
// gate that simulation accounts don't have, and the keeper unit tests cover
// the real path.
func SimulateMsgMakeCollectionPermanent(
	_ types.AuthKeeper,
	_ types.BankKeeper,
	_ keeper.Keeper,
	_ client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgMakeCollectionPermanent{Creator: simAccount.Address.String()}
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "make-permanent requires trust level verification"), nil, nil
	}
}
