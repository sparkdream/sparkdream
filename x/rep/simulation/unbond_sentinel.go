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

// SimulateMsgUnbondSentinel uses a direct keeper write to bypass DREAM
// unlocking; full token integration is covered by integration tests.
func SimulateMsgUnbondSentinel(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		activity, err := k.SentinelActivity.Get(ctx, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondSentinel{}), "no sentinel to unbond"), nil, nil
		}

		if activity.CurrentBond == "" || activity.CurrentBond == "0" {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondSentinel{}), "no bond to unbond"), nil, nil
		}

		activity.CurrentBond = "0"
		activity.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_UNSPECIFIED

		if err := k.SentinelActivity.Set(ctx, simAccount.Address.String(), activity); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondSentinel{}), "failed to update sentinel activity"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondSentinel{}), "ok (direct keeper call)"), nil, nil
	}
}
