package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgBondSentinel simulates a MsgBondSentinel message using direct keeper calls.
// This bypasses the DREAM token requirement for simulation purposes.
// Full token integration testing should be done in integration tests.
func SimulateMsgBondSentinel(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Use direct keeper calls to create sentinel activity (bypasses DREAM token transfer)
		bondAmount := fmt.Sprintf("%d", 100+r.Intn(900))

		activity := types.SentinelActivity{
			Address:            simAccount.Address.String(),
			CurrentBond:        bondAmount,
			BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
			TotalHides:         0,
			UpheldHides:        0,
			OverturnedHides:    0,
			CumulativeRewards:  "0",
			TotalCommittedBond: "0",
		}

		if err := k.SentinelActivity.Set(ctx, simAccount.Address.String(), activity); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgBondSentinel{}), "failed to create sentinel activity"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgBondSentinel{}), "ok (direct keeper call)"), nil, nil
	}
}
