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

func SimulateMsgAcceptMigration(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Accepting leads to finalization, which turns the old sim account into
		// a tombstone. The simulator keeps a fixed account set and every other
		// module's op may then pick that account as a signer, which the
		// TombstonedSignerDecorator refuses and the simulation counts as a
		// failed delivery. Finalization is covered by the keeper unit tests
		// and test/rep/member_migration_test.sh; the simulation exercises
		// request and cancel.
		_ = k
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptMigration{}), "finalization would tombstone a sim account; not simulated"), nil, nil
	}
}
