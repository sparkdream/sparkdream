package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

// SimulateMsgUpdateVault is a no-op: MsgUpdateVault is signed by the vault
// address, for which no key exists, so it only ever runs as the inner message
// of an executed proposal (see SimulateMsgPropose).
func SimulateMsgUpdateVault(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateVault{}), "vault-signed; exercised through proposals"), nil, nil
	}
}
