package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUnbondSentinel simulates a MsgUnbondSentinel message using direct keeper calls.
// This bypasses the DREAM token requirement for simulation purposes.
// Full token integration testing should be done in integration tests.
func SimulateMsgUnbondSentinel(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Check if this account is a bonded sentinel
		activity, err := k.SentinelActivity.Get(ctx, simAccount.Address.String())
		if err != nil {
			// Create a bonded sentinel first
			if err := getOrCreateBondedSentinel(r, ctx, k, simAccount.Address.String()); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondSentinel{}), "failed to create sentinel"), nil, nil
			}
			activity, _ = k.SentinelActivity.Get(ctx, simAccount.Address.String())
		}

		// Check if there's any bond to unbond
		if activity.CurrentBond == "" || activity.CurrentBond == "0" {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondSentinel{}), "no bond to unbond"), nil, nil
		}

		// Use direct keeper calls to unbond (bypasses DREAM token transfer)
		// Set bond to 0 and status to UNSPECIFIED (no active bond)
		activity.CurrentBond = "0"
		activity.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_UNSPECIFIED

		if err := k.SentinelActivity.Set(ctx, simAccount.Address.String(), activity); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondSentinel{}), "failed to update sentinel activity"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondSentinel{}), "ok (direct keeper call)"), nil, nil
	}
}
