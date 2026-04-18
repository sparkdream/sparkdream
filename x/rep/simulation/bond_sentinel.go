package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgBondSentinel uses a direct keeper write to bypass the DREAM
// locking flow; full token integration is covered by integration tests.
func SimulateMsgBondSentinel(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		bondAmount := fmt.Sprintf("%d", 100+r.Intn(900))
		activity := types.SentinelActivity{
			Address:            simAccount.Address.String(),
			CurrentBond:        bondAmount,
			BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
			CumulativeRewards:  "0",
			TotalCommittedBond: "0",
		}

		if err := k.SentinelActivity.Set(ctx, simAccount.Address.String(), activity); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgBondSentinel{}), "failed to create sentinel activity"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgBondSentinel{}), "ok (direct keeper call)"), nil, nil
	}
}
