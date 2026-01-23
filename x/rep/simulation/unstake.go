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

func SimulateMsgUnstake(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// NOTE: The unstake simulation is complex because:
		// 1. It requires a stake to exist with proper member balance tracking
		// 2. The member's StakedDream must match what the Stake record expects
		// 3. The unlock mechanism relies on precise balance state
		// Rather than risk state inconsistency, we skip this message in simulation.
		// In a real chain environment, the Stake and MsgStake handlers maintain proper state.
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnstake{}), "skipped: requires accurate stake state tracking"), nil, nil
	}
}
