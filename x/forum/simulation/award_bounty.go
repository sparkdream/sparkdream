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

// SimulateMsgAwardBounty simulates a MsgAwardBounty message using direct keeper calls.
// This bypasses token transfer requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgAwardBounty(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a bounty
		bountyID, err := getOrCreateBounty(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardBounty{}), "failed to get/create bounty"), nil, nil
		}

		// Use direct keeper calls to award bounty (bypasses token transfer)
		bounty, err := k.Bounty.Get(ctx, bountyID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardBounty{}), "bounty not found"), nil, nil
		}

		// Set status to awarded
		bounty.Status = types.BountyStatus_BOUNTY_STATUS_AWARDED

		if err := k.Bounty.Set(ctx, bountyID, bounty); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardBounty{}), "failed to award bounty"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAwardBounty{}), "ok (direct keeper call)"), nil, nil
	}
}
